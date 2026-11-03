// Package engine executes the steps of a Book and reports what happened.
//
// It is the single implementation shared by the CLI and the HTTP API: both
// call Runner.Run and receive the same StepResults and event stream.
package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kingzad/drill/internal/book"
)

// DefaultTimeout applies when neither the book nor the step sets a timeout.
const DefaultTimeout = 10 * time.Minute

// Step statuses.
const (
	StatusPassed  = "passed"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusPreview = "preview" // dry run: nothing was executed
)

// Event is one item in a live run stream.
type Event struct {
	Type       string `json:"type"` // run-start, step-start, output, step-end, run-end
	BookID     string `json:"bookId,omitempty"`
	Title      string `json:"title,omitempty"`
	Index      int    `json:"index,omitempty"`
	Total      int    `json:"total,omitempty"`
	StepID     string `json:"stepId,omitempty"`
	Shell      string `json:"shell,omitempty"`
	Body       string `json:"body,omitempty"`
	IsScript   bool   `json:"isScript,omitempty"`
	DryRun     bool   `json:"dryRun,omitempty"`
	Stream     string `json:"stream,omitempty"` // stdout, stderr
	Text       string `json:"text,omitempty"`
	Status     string `json:"status,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
	OK         bool   `json:"ok,omitempty"`
	Error      string `json:"error,omitempty"`
}

// StepResult is the outcome of one step.
type StepResult struct {
	Index      int    `json:"index"`
	StepID     string `json:"stepId"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Shell      string `json:"shell,omitempty"`
	Body       string `json:"body,omitempty"`
	IsScript   bool   `json:"isScript,omitempty"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMS int64  `json:"durationMs"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RunResult is the outcome of a whole run.
type RunResult struct {
	BookID     string       `json:"bookId"`
	DryRun     bool         `json:"dryRun"`
	OK         bool         `json:"ok"`
	StartedAt  time.Time    `json:"startedAt"`
	DurationMS int64        `json:"durationMs"`
	Steps      []StepResult `json:"steps"`
}

// Options control a run.
type Options struct {
	StepIDs []string // empty runs every step, in book order
	DryRun  bool     // resolve and report, execute nothing
}

// Runner executes steps relative to Dir, the directory holding the book file.
type Runner struct {
	Dir  string
	Sink func(Event) // optional live event stream
}

func (r *Runner) emit(ev Event) {
	if r.Sink != nil {
		r.Sink(ev)
	}
}

// Run executes the selected steps. A failing step stops the run unless the
// step sets continueOnError. An error is returned only when the run itself
// could not start (for example an unknown step id); step failures are
// reported in the RunResult.
func (r *Runner) Run(ctx context.Context, b *book.Book, opts Options) (*RunResult, error) {
	picked, err := pickSteps(b, opts.StepIDs)
	if err != nil {
		return nil, err
	}

	started := time.Now()
	res := &RunResult{BookID: b.ID, DryRun: opts.DryRun, StartedAt: started, Steps: []StepResult{}}
	r.emit(Event{Type: "run-start", BookID: b.ID, Title: b.Title, Total: len(picked), DryRun: opts.DryRun})

	halted := false
	for _, idx := range picked {
		step := b.Steps[idx]
		if halted {
			sr := StepResult{Index: idx, StepID: step.ID, Title: step.Title, Status: StatusSkipped}
			res.Steps = append(res.Steps, sr)
			r.emit(Event{Type: "step-end", Index: idx, StepID: step.ID, Title: step.Title, Status: StatusSkipped})
			continue
		}
		sr := r.runStep(ctx, b, step, idx, opts.DryRun)
		res.Steps = append(res.Steps, sr)
		if sr.Status == StatusFailed && !step.ContinueOnError {
			halted = true
		}
		if ctx.Err() != nil {
			halted = true
		}
	}

	res.DurationMS = time.Since(started).Milliseconds()
	res.OK = true
	for _, sr := range res.Steps {
		if sr.Status == StatusFailed {
			res.OK = false
			break
		}
	}
	r.emit(Event{Type: "run-end", BookID: b.ID, OK: res.OK, DurationMS: res.DurationMS, DryRun: opts.DryRun})
	return res, nil
}

func pickSteps(b *book.Book, ids []string) ([]int, error) {
	if len(ids) == 0 {
		out := make([]int, len(b.Steps))
		for i := range b.Steps {
			out[i] = i
		}
		return out, nil
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var out []int
	for i, s := range b.Steps {
		if wanted[s.ID] {
			out = append(out, i)
			delete(wanted, s.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("unknown step id(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

func (r *Runner) runStep(ctx context.Context, b *book.Book, step book.Step, idx int, dryRun bool) StepResult {
	started := time.Now()
	res := StepResult{
		Index:    idx,
		StepID:   step.ID,
		Title:    step.Title,
		Shell:    resolveShell(b, step),
		Body:     stepBody(step),
		IsScript: step.Script != "",
	}
	finish := func() StepResult {
		res.DurationMS = time.Since(started).Milliseconds()
		r.emit(Event{
			Type: "step-end", Index: res.Index, StepID: res.StepID, Title: res.Title,
			Status: res.Status, ExitCode: res.ExitCode, TimedOut: res.TimedOut,
			DurationMS: res.DurationMS, Error: res.Error,
		})
		return res
	}
	fail := func(format string, args ...any) StepResult {
		res.Status = StatusFailed
		res.Error = fmt.Sprintf(format, args...)
		return finish()
	}

	spec, err := specFor(res.Shell)
	if err != nil {
		return fail("%v", err)
	}
	res.Shell = spec.name

	dir, err := resolveDir(r.Dir, b, step)
	if err != nil {
		return fail("%v", err)
	}

	r.emit(Event{
		Type: "step-start", Index: idx, StepID: step.ID, Title: step.Title,
		Shell: spec.name, Body: res.Body, IsScript: res.IsScript,
	})

	if dryRun {
		res.Status = StatusPreview
		return finish()
	}

	args := append([]string{}, spec.commandArgs...)
	var scriptPath string
	if step.Script != "" {
		f, err := os.CreateTemp("", "drill-*"+spec.ext)
		if err != nil {
			return fail("could not create temporary script: %v", err)
		}
		scriptPath = f.Name()
		defer os.Remove(scriptPath)
		if _, err := f.WriteString(step.Script); err != nil {
			f.Close()
			return fail("could not write temporary script: %v", err)
		}
		if err := f.Close(); err != nil {
			return fail("could not write temporary script: %v", err)
		}
		args = append([]string{}, spec.fileArgs...)
		args = append(args, scriptPath)
	} else {
		args = append(args, step.Command)
	}

	timeout := resolveTimeout(b, step)
	runCtx, cancel := ctx, func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	outR, outW, err := os.Pipe()
	if err != nil {
		return fail("could not create pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return fail("could not create pipe: %v", err)
	}

	cmd := exec.Command(spec.prog, args...)
	cmd.Dir = dir
	cmd.Stdout = outW
	cmd.Stderr = errW
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		return fail("could not start %s: %v", spec.prog, err)
	}
	// The child owns the write ends now; close ours so readers see EOF once
	// every writer (including grandchildren) exits.
	outW.Close()
	errW.Close()

	stopped := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			_ = killTree(cmd)
		case <-stopped:
		}
	}()

	var mu sync.Mutex
	var stdout, stderr strings.Builder
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); r.drain(outR, "stdout", step.ID, &mu, &stdout) }()
	go func() { defer readers.Done(); r.drain(errR, "stderr", step.ID, &mu, &stderr) }()

	waitErr := make(chan error, 1)
	go func() {
		readers.Wait()
		waitErr <- cmd.Wait()
	}()

	var werr error
	select {
	case werr = <-waitErr:
	case <-runCtx.Done():
		_ = killTree(cmd)
		werr = <-waitErr
	}
	close(stopped)

	mu.Lock()
	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	mu.Unlock()

	switch {
	case werr == nil:
		code := 0
		res.ExitCode = &code
		res.Status = StatusPassed
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.Status = StatusFailed
		res.TimedOut = true
		res.Error = fmt.Sprintf("timed out after %s", timeout)
		if code, ok := exitCode(werr); ok {
			res.ExitCode = &code
		}
	default:
		res.Status = StatusFailed
		if code, ok := exitCode(werr); ok {
			res.ExitCode = &code
		} else {
			res.Error = werr.Error()
		}
		if ctx.Err() != nil {
			res.Error = "canceled"
		}
	}
	return finish()
}

// drain forwards a process stream line by line and keeps a copy.
func (r *Runner) drain(rd io.ReadCloser, stream, stepID string, mu *sync.Mutex, sb *strings.Builder) {
	defer rd.Close()
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		mu.Lock()
		sb.WriteString(line)
		sb.WriteByte('\n')
		mu.Unlock()
		r.emit(Event{Type: "output", StepID: stepID, Stream: stream, Text: line})
	}
}

func exitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

func resolveDir(base string, b *book.Book, step book.Step) (string, error) {
	rel := step.Cwd
	if rel == "" {
		rel = b.Defaults.Cwd
	}
	dir := base
	if rel != "" {
		if filepath.IsAbs(rel) {
			dir = rel
		} else {
			dir = filepath.Join(base, rel)
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %s is not a directory", dir)
	}
	return dir, nil
}

func resolveTimeout(b *book.Book, step book.Step) time.Duration {
	secs := step.TimeoutSeconds
	if secs == 0 {
		secs = b.Defaults.TimeoutSeconds
	}
	if secs <= 0 {
		return DefaultTimeout
	}
	return time.Duration(secs) * time.Second
}

func resolveShell(b *book.Book, step book.Step) string {
	if step.Shell != "" {
		return strings.ToLower(step.Shell)
	}
	if b.Defaults.Shell != "" {
		return strings.ToLower(b.Defaults.Shell)
	}
	return "default"
}

func stepBody(step book.Step) string {
	if step.Script != "" {
		return step.Script
	}
	return step.Command
}

// shellSpec describes how to turn a step body into a process invocation.
type shellSpec struct {
	name        string
	prog        string
	commandArgs []string // args placed before the inline command
	fileArgs    []string // args placed before the script file path
	ext         string   // temporary script file extension
}

func specFor(shell string) (shellSpec, error) {
	switch shell {
	case "default":
		return specFor(defaultShell())
	case "sh":
		return shellSpec{name: "sh", prog: "sh", commandArgs: []string{"-c"}, ext: ".sh"}, nil
	case "bash":
		return shellSpec{name: "bash", prog: "bash", commandArgs: []string{"-c"}, ext: ".sh"}, nil
	case "pwsh":
		return shellSpec{
			name: "pwsh", prog: "pwsh",
			commandArgs: []string{"-NoProfile", "-NonInteractive", "-Command"},
			fileArgs:    []string{"-NoProfile", "-NonInteractive", "-File"},
			ext:         ".ps1",
		}, nil
	case "powershell":
		return shellSpec{
			name: "powershell", prog: "powershell",
			commandArgs: []string{"-NoProfile", "-NonInteractive", "-Command"},
			fileArgs:    []string{"-NoProfile", "-NonInteractive", "-File"},
			ext:         ".ps1",
		}, nil
	case "cmd":
		return shellSpec{name: "cmd", prog: "cmd", commandArgs: []string{"/C"}, fileArgs: []string{"/C"}, ext: ".cmd"}, nil
	case "python", "python3":
		return shellSpec{name: shell, prog: shell, commandArgs: []string{"-c"}, ext: ".py"}, nil
	}
	return shellSpec{}, fmt.Errorf("unknown shell %q", shell)
}

func defaultShell() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err == nil {
			return "pwsh"
		}
		return "cmd"
	}
	return "sh"
}
