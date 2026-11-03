package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kingzad/drill/internal/book"
)

func requirePOSIX(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these cases use a POSIX shell")
	}
}

func testBook(steps ...book.Step) *book.Book {
	return &book.Book{Version: book.SchemaVersion, ID: "test", Title: "Test", Steps: steps}
}

func runBook(t *testing.T, b *book.Book, opts Options) *RunResult {
	t.Helper()
	res, err := (&Runner{Dir: t.TempDir()}).Run(context.Background(), b, opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestRunCapturesStreamsAndExitCode(t *testing.T) {
	requirePOSIX(t)
	b := testBook(book.Step{
		ID:      "s1",
		Title:   "Mixed streams",
		Command: `printf 'out\n'; printf 'err\n' >&2; exit 7`,
	})
	res := runBook(t, b, Options{})

	got := res.Steps[0]
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 7 {
		t.Errorf("exit code = %v, want 7", got.ExitCode)
	}
	if got.Stdout != "out\n" {
		t.Errorf("stdout = %q, want %q", got.Stdout, "out\n")
	}
	if got.Stderr != "err\n" {
		t.Errorf("stderr = %q, want %q", got.Stderr, "err\n")
	}
	if res.OK {
		t.Error("run should not be OK")
	}
}

func TestRunSucceeds(t *testing.T) {
	requirePOSIX(t)
	b := testBook(book.Step{ID: "s1", Title: "Say hi", Command: "echo hi"})
	res := runBook(t, b, Options{})

	if !res.OK {
		t.Fatal("run should succeed")
	}
	if res.Steps[0].Status != StatusPassed {
		t.Errorf("status = %q, want passed", res.Steps[0].Status)
	}
	if res.Steps[0].ExitCode == nil || *res.Steps[0].ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", res.Steps[0].ExitCode)
	}
	if res.Steps[0].Stdout != "hi\n" {
		t.Errorf("stdout = %q", res.Steps[0].Stdout)
	}
}

func TestRunInlineScript(t *testing.T) {
	requirePOSIX(t)
	b := testBook(book.Step{ID: "s1", Title: "Script", Script: "echo one\necho two\n"})
	res := runBook(t, b, Options{})

	if res.Steps[0].Status != StatusPassed {
		t.Fatalf("script failed: %+v", res.Steps[0])
	}
	if res.Steps[0].Stdout != "one\ntwo\n" {
		t.Errorf("stdout = %q", res.Steps[0].Stdout)
	}
	if !res.Steps[0].IsScript {
		t.Error("IsScript should be true")
	}
}

func TestFailFastSkipsRemainingSteps(t *testing.T) {
	requirePOSIX(t)
	b := testBook(
		book.Step{ID: "a", Title: "A", Command: "true"},
		book.Step{ID: "b", Title: "B", Command: "exit 1"},
		book.Step{ID: "c", Title: "C", Command: "echo should-not-run"},
	)
	res := runBook(t, b, Options{})

	if len(res.Steps) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res.Steps))
	}
	if res.Steps[0].Status != StatusPassed {
		t.Errorf("step a = %q", res.Steps[0].Status)
	}
	if res.Steps[1].Status != StatusFailed {
		t.Errorf("step b = %q", res.Steps[1].Status)
	}
	if res.Steps[2].Status != StatusSkipped {
		t.Errorf("step c = %q, want skipped", res.Steps[2].Status)
	}
	if res.Steps[2].Stdout != "" {
		t.Errorf("skipped step produced output: %q", res.Steps[2].Stdout)
	}
	if res.OK {
		t.Error("run should not be OK")
	}
}

func TestContinueOnError(t *testing.T) {
	requirePOSIX(t)
	b := testBook(
		book.Step{ID: "a", Title: "A", Command: "exit 4", ContinueOnError: true},
		book.Step{ID: "b", Title: "B", Command: "echo ran"},
	)
	res := runBook(t, b, Options{})

	if res.Steps[0].Status != StatusFailed {
		t.Errorf("step a = %q", res.Steps[0].Status)
	}
	if res.Steps[1].Status != StatusPassed {
		t.Errorf("step b should have run, got %q", res.Steps[1].Status)
	}
	if res.OK {
		t.Error("run with a failed step should not be OK")
	}
}

func TestDryRunExecutesNothing(t *testing.T) {
	requirePOSIX(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")
	b := testBook(book.Step{ID: "s1", Title: "Touch", Command: "touch " + marker})

	res, err := (&Runner{Dir: dir}).Run(context.Background(), b, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Steps[0].Status != StatusPreview {
		t.Errorf("status = %q, want preview", res.Steps[0].Status)
	}
	if !res.OK {
		t.Error("a dry run should be OK")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("dry run executed the command")
	}
}

func TestTimeoutKillsProcessTree(t *testing.T) {
	requirePOSIX(t)
	b := testBook(book.Step{
		ID:             "s1",
		Title:          "Hang",
		Command:        "sleep 30 & sleep 30",
		TimeoutSeconds: 1,
	})

	started := time.Now()
	res, err := (&Runner{Dir: t.TempDir()}).Run(context.Background(), b, Options{})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := res.Steps[0]
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !got.TimedOut {
		t.Error("TimedOut should be true")
	}
	if !strings.Contains(got.Error, "timed out") {
		t.Errorf("error = %q", got.Error)
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took %s; the process tree was not killed", elapsed)
	}
}

func TestUnknownStepIDIsAnError(t *testing.T) {
	b := testBook(book.Step{ID: "a", Title: "A"})
	_, err := (&Runner{Dir: t.TempDir()}).Run(context.Background(), b, Options{StepIDs: []string{"nope", "also-nope"}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "unknown step id") {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "also-nope, nope") {
		t.Errorf("ids should be sorted in the message: %v", err)
	}
}

func TestStepSelectionKeepsBookOrder(t *testing.T) {
	requirePOSIX(t)
	b := testBook(
		book.Step{ID: "a", Title: "A", Command: "echo a"},
		book.Step{ID: "b", Title: "B", Command: "echo b"},
		book.Step{ID: "c", Title: "C", Command: "echo c"},
	)
	res := runBook(t, b, Options{StepIDs: []string{"c", "a"}})

	if len(res.Steps) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res.Steps))
	}
	if res.Steps[0].StepID != "a" || res.Steps[1].StepID != "c" {
		t.Errorf("order = %s, %s; want a, c", res.Steps[0].StepID, res.Steps[1].StepID)
	}
}

func TestWorkingDirOverride(t *testing.T) {
	requirePOSIX(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	b := &book.Book{
		Version:  book.SchemaVersion,
		ID:       "t",
		Title:    "T",
		Defaults: book.Defaults{Cwd: "sub"},
		Steps:    []book.Step{{ID: "s1", Title: "Pwd", Command: "pwd"}},
	}
	res, err := (&Runner{Dir: dir}).Run(context.Background(), b, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Steps[0].Status != StatusPassed {
		t.Fatalf("step failed: %+v", res.Steps[0])
	}

	got, err := filepath.EvalSymlinks(strings.TrimSpace(res.Steps[0].Stdout))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("cwd = %q, want %q", got, want)
	}
}

func TestMissingWorkingDirFails(t *testing.T) {
	b := testBook(book.Step{ID: "s1", Title: "Pwd", Command: "pwd", Cwd: "does-not-exist"})
	res := runBook(t, b, Options{})
	if res.Steps[0].Status != StatusFailed {
		t.Fatalf("status = %q", res.Steps[0].Status)
	}
	if !strings.Contains(res.Steps[0].Error, "working directory") {
		t.Errorf("error = %q", res.Steps[0].Error)
	}
}

func TestShellOverride(t *testing.T) {
	requirePOSIX(t)
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	b := testBook(book.Step{ID: "s1", Title: "Bash", Command: "echo ${BASH_VERSION:?}", Shell: "bash"})
	res := runBook(t, b, Options{})
	if res.Steps[0].Status != StatusPassed {
		t.Fatalf("bash step failed: %+v", res.Steps[0])
	}
	if strings.TrimSpace(res.Steps[0].Stdout) == "" {
		t.Error("BASH_VERSION was empty, so bash was probably not used")
	}
}

func TestUnknownShellFailsStep(t *testing.T) {
	b := testBook(book.Step{ID: "s1", Title: "Fish", Command: "echo hi", Shell: "fish"})
	res := runBook(t, b, Options{})
	if res.Steps[0].Status != StatusFailed {
		t.Fatalf("status = %q", res.Steps[0].Status)
	}
	if !strings.Contains(res.Steps[0].Error, "unknown shell") {
		t.Errorf("error = %q", res.Steps[0].Error)
	}
}

func TestEventStream(t *testing.T) {
	requirePOSIX(t)
	var mu sync.Mutex
	var events []Event

	b := testBook(book.Step{ID: "s1", Title: "Echo", Command: "echo hello"})
	runner := &Runner{
		Dir: t.TempDir(),
		Sink: func(ev Event) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	}
	if _, err := runner.Run(context.Background(), b, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) < 4 {
		t.Fatalf("expected run-start, step-start, output, step-end, run-end; got %d events", len(events))
	}
	if events[0].Type != "run-start" || events[0].Total != 1 {
		t.Errorf("first event = %+v", events[0])
	}
	if events[len(events)-1].Type != "run-end" || !events[len(events)-1].OK {
		t.Errorf("last event = %+v", events[len(events)-1])
	}

	var sawStart, sawOutput, sawEnd bool
	for _, ev := range events {
		switch ev.Type {
		case "step-start":
			sawStart = ev.StepID == "s1" && ev.Shell == "sh"
		case "output":
			sawOutput = sawOutput || ev.Text == "hello" && ev.Stream == "stdout"
		case "step-end":
			sawEnd = ev.Status == StatusPassed && ev.ExitCode != nil && *ev.ExitCode == 0
		}
	}
	if !sawStart || !sawOutput || !sawEnd {
		t.Errorf("missing events: start=%v output=%v end=%v", sawStart, sawOutput, sawEnd)
	}
}

func TestResolveTimeoutPrecedence(t *testing.T) {
	tenMinutes := DefaultTimeout
	tests := []struct {
		name string
		book book.Defaults
		step book.Step
		want time.Duration
	}{
		{"default", book.Defaults{}, book.Step{}, tenMinutes},
		{"book", book.Defaults{TimeoutSeconds: 30}, book.Step{}, 30 * time.Second},
		{"step wins", book.Defaults{TimeoutSeconds: 30}, book.Step{TimeoutSeconds: 5}, 5 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &book.Book{Defaults: tc.book}
			if got := resolveTimeout(b, tc.step); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSpecFor(t *testing.T) {
	tests := []struct {
		shell   string
		prog    string
		command string
		ext     string
	}{
		{"sh", "sh", "-c", ".sh"},
		{"bash", "bash", "-c", ".sh"},
		{"pwsh", "pwsh", "-Command", ".ps1"},
		{"powershell", "powershell", "-Command", ".ps1"},
		{"cmd", "cmd", "/C", ".cmd"},
		{"python", "python", "-c", ".py"},
	}
	for _, tc := range tests {
		spec, err := specFor(tc.shell)
		if err != nil {
			t.Fatalf("specFor(%q): %v", tc.shell, err)
		}
		if spec.prog != tc.prog || spec.ext != tc.ext {
			t.Errorf("specFor(%q) = %+v", tc.shell, spec)
		}
		found := false
		for _, arg := range spec.commandArgs {
			found = found || arg == tc.command
		}
		if !found {
			t.Errorf("specFor(%q) commandArgs = %v, want %q", tc.shell, spec.commandArgs, tc.command)
		}
	}
	if _, err := specFor("fish"); err == nil {
		t.Error("expected an error for an unknown shell")
	}
}
