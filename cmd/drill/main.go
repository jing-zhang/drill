// Command drill drives Books from a terminal, a script, CI, or an AI agent.
// It uses the same book/store/engine packages as the desktop app.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/kingzad/drill/internal/book"
	"github.com/kingzad/drill/internal/engine"
	"github.com/kingzad/drill/internal/store"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "list", "ls":
		return cmdList(rest)
	case "show", "cat":
		return cmdShow(rest)
	case "new", "create":
		return cmdNew(rest)
	case "add":
		return cmdAdd(rest)
	case "update", "set":
		return cmdUpdate(rest)
	case "remove", "rm":
		return cmdRemove(rest)
	case "move", "mv":
		return cmdMove(rest)
	case "validate", "check":
		return cmdValidate(rest)
	case "run":
		return cmdRun(rest)
	case "version", "--version", "-v":
		fmt.Println("drill " + version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "drill: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `drill — runbooks as JSON

Usage:
  drill <command> [flags] [args]

Books
  list      List books in a folder                [--dir DIR] [--json]
  show      Print one book as JSON                <book>
  new       Create an empty book                  <id|title> [--title T] [--description D]
  validate  Validate books                        [book...] [--dir DIR]

Steps
  add       Append (or insert) a step             <book> --title T [--command C | --script S | --script-file F]
  update    Change fields of a step               <book> <step-id> [flags]
  remove    Delete a step                         <book> <step-id>
  move      Reorder a step                        <book> <step-id> --to N

Running
  run       Execute a book                        <book> [--step ID ...] [--dry-run] [--json]

Flags common to every command:
  --dir DIR   folder holding the book JSON files (default: $DRILL_DIR or ".")

A <book> is either a book id (release-1-4-0) or a path to a .json file.
`)
}

// parseArgs parses flags even when they follow positional arguments. The
// standard flag package stops at the first non-flag, so `drill add my-book
// --title "Tests"` would otherwise ignore every flag. Flags and their values are
// moved ahead of the positional arguments before parsing.
func parseArgs(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			// Keep the terminator so Parse stops treating the rest as flags.
			flags = append(flags, "--")
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(arg) > 1 && arg[0] == '-':
			flags = append(flags, arg)
			name := strings.TrimLeft(arg, "-")
			if strings.Contains(name, "=") {
				continue // value is inline
			}
			if f := fs.Lookup(name); f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					continue
				}
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, arg)
		}
	}
	return fs.Parse(append(flags, positional...))
}

func defaultDir() string {
	if d := os.Getenv("DRILL_DIR"); d != "" {
		return d
	}
	return "."
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "drill: %v\n", err)
	return 1
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return 0
}

func newStore(dir string) (*store.Store, error) { return store.New(dir) }

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}

	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	listing, err := st.List()
	if err != nil {
		return fail(err)
	}

	if *asJSON {
		type row struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Steps int    `json:"steps"`
			Path  string `json:"path"`
		}
		rows := []row{}
		for _, e := range listing.Entries {
			rows = append(rows, row{e.Book.ID, e.Book.Title, len(e.Book.Steps), e.Path})
		}
		return printJSON(map[string]any{
			"dir":      st.Dir,
			"books":    rows,
			"problems": listing.Problems,
		})
	}

	if len(listing.Entries) == 0 {
		fmt.Printf("no books in %s\n", st.Dir)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tSTEPS\tFILE")
	for _, e := range listing.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", e.Book.ID, e.Book.Title, len(e.Book.Steps), e.Path)
	}
	tw.Flush()

	for _, p := range listing.Problems {
		fmt.Fprintf(os.Stderr, "invalid: %s\n", p.Error)
	}
	if len(listing.Problems) > 0 {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

func cmdShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		return fail(fmt.Errorf("usage: drill show <book>"))
	}
	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	data, err := book.Marshal(e.Book)
	if err != nil {
		return fail(err)
	}
	os.Stdout.Write(data)
	return 0
}

// ---------------------------------------------------------------------------
// new
// ---------------------------------------------------------------------------

func cmdNew(args []string) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	title := fs.String("title", "", "book title (default: derived from the id)")
	description := fs.String("description", "", "markdown description")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		return fail(fmt.Errorf("usage: drill new <id|title> [--title T] [--description D]"))
	}

	id := fs.Arg(0)
	if !book.ValidID(id) {
		id = book.Slug(id)
	}
	if id == "" {
		return fail(fmt.Errorf("could not derive an id from %q", fs.Arg(0)))
	}

	b := &book.Book{
		Version:     book.SchemaVersion,
		ID:          id,
		Title:       *title,
		Description: *description,
		Steps:       []book.Step{},
	}
	if strings.TrimSpace(b.Title) == "" {
		b.Title = humanize(id)
	}

	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	path, err := st.Create(b)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("created %s\n", path)
	return 0
}

// ---------------------------------------------------------------------------
// add
// ---------------------------------------------------------------------------

func cmdAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	id := fs.String("id", "", "step id (default: derived from the title)")
	title := fs.String("title", "", "step title (required)")
	description := fs.String("description", "", "markdown description")
	command := fs.String("command", "", "shell command to run")
	script := fs.String("script", "", "inline script to run ('-' reads stdin)")
	scriptFile := fs.String("script-file", "", "read the script from this file")
	shell := fs.String("shell", "", "interpreter: sh, bash, pwsh, powershell, cmd, python")
	cwd := fs.String("cwd", "", "working directory, relative to the book")
	timeout := fs.Int("timeout", 0, "timeout in seconds")
	continueOnError := fs.Bool("continue-on-error", false, "keep going if this step fails")
	at := fs.Int("at", 0, "insert as this 1-based position (default: append)")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		return fail(fmt.Errorf("usage: drill add <book> --title T [--command C | --script S]"))
	}
	if strings.TrimSpace(*title) == "" {
		return fail(fmt.Errorf("--title is required"))
	}

	body, err := resolveScript(*script, *scriptFile)
	if err != nil {
		return fail(err)
	}
	if *command != "" && body != "" {
		return fail(fmt.Errorf("use either --command or --script, not both"))
	}

	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}

	step := book.Step{
		ID:              *id,
		Title:           strings.TrimSpace(*title),
		Description:     *description,
		Command:         *command,
		Script:          body,
		Shell:           *shell,
		Cwd:             *cwd,
		TimeoutSeconds:  *timeout,
		ContinueOnError: *continueOnError,
	}
	if step.ID == "" {
		step.ID = book.UniqueStepID(e.Book, step.Title)
	}
	if !book.ValidID(step.ID) {
		return fail(fmt.Errorf("%q is not a valid step id", step.ID))
	}
	if e.Book.HasStepID(step.ID) {
		return fail(fmt.Errorf("a step with id %q already exists", step.ID))
	}

	if *at <= 0 || *at > len(e.Book.Steps) {
		e.Book.Steps = append(e.Book.Steps, step)
	} else {
		pos := *at - 1
		e.Book.Steps = append(e.Book.Steps, book.Step{})
		copy(e.Book.Steps[pos+1:], e.Book.Steps[pos:])
		e.Book.Steps[pos] = step
	}

	if err := st.SaveTo(e.Path, e.Book); err != nil {
		return fail(err)
	}
	fmt.Printf("added step %q to %s\n", step.ID, e.Book.ID)
	return 0
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	title := fs.String("title", "", "step title")
	description := fs.String("description", "", "markdown description")
	command := fs.String("command", "", "shell command (clears any script)")
	script := fs.String("script", "", "inline script (clears any command; '-' reads stdin)")
	scriptFile := fs.String("script-file", "", "read the script from this file")
	shell := fs.String("shell", "", "interpreter name ('' resets to the book default)")
	cwd := fs.String("cwd", "", "working directory, relative to the book")
	timeout := fs.Int("timeout", 0, "timeout in seconds")
	continueOnError := fs.Bool("continue-on-error", false, "keep going if this step fails")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		return fail(fmt.Errorf("usage: drill update <book> <step-id> [flags]"))
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	stepID := fs.Arg(1)
	idx := e.Book.StepIndex(stepID)
	if idx < 0 {
		return fail(fmt.Errorf("step %q not found", stepID))
	}
	step := e.Book.Steps[idx]

	if set["title"] {
		if strings.TrimSpace(*title) == "" {
			return fail(fmt.Errorf("--title cannot be empty"))
		}
		step.Title = strings.TrimSpace(*title)
	}
	if set["description"] {
		step.Description = *description
	}
	if set["command"] {
		step.Command = *command
		step.Script = ""
	}
	if set["script"] || set["script-file"] {
		body, err := resolveScript(*script, *scriptFile)
		if err != nil {
			return fail(err)
		}
		step.Script = body
		step.Command = ""
	}
	if set["shell"] {
		step.Shell = *shell
	}
	if set["cwd"] {
		step.Cwd = *cwd
	}
	if set["timeout"] {
		step.TimeoutSeconds = *timeout
	}
	if set["continue-on-error"] {
		step.ContinueOnError = *continueOnError
	}

	e.Book.Steps[idx] = step
	if err := st.SaveTo(e.Path, e.Book); err != nil {
		return fail(err)
	}
	fmt.Printf("updated step %q in %s\n", step.ID, e.Book.ID)
	return 0
}

// ---------------------------------------------------------------------------
// remove / move
// ---------------------------------------------------------------------------

func cmdRemove(args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		return fail(fmt.Errorf("usage: drill remove <book> <step-id>"))
	}
	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	stepID := fs.Arg(1)
	idx := e.Book.StepIndex(stepID)
	if idx < 0 {
		return fail(fmt.Errorf("step %q not found", stepID))
	}
	e.Book.Steps = append(e.Book.Steps[:idx], e.Book.Steps[idx+1:]...)
	if err := st.SaveTo(e.Path, e.Book); err != nil {
		return fail(err)
	}
	fmt.Printf("removed step %q from %s\n", stepID, e.Book.ID)
	return 0
}

func cmdMove(args []string) int {
	fs := flag.NewFlagSet("move", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	to := fs.Int("to", 0, "new 1-based position")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 2 || *to <= 0 {
		return fail(fmt.Errorf("usage: drill move <book> <step-id> --to N"))
	}
	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}
	stepID := fs.Arg(1)
	from := e.Book.StepIndex(stepID)
	if from < 0 {
		return fail(fmt.Errorf("step %q not found", stepID))
	}
	target := *to - 1
	if target >= len(e.Book.Steps) {
		return fail(fmt.Errorf("position %d is past the end of the book (%d steps)", *to, len(e.Book.Steps)))
	}
	step := e.Book.Steps[from]
	steps := append(e.Book.Steps[:from], e.Book.Steps[from+1:]...)
	steps = append(steps, book.Step{})
	copy(steps[target+1:], steps[target:])
	steps[target] = step
	e.Book.Steps = steps
	if err := st.SaveTo(e.Path, e.Book); err != nil {
		return fail(err)
	}
	fmt.Printf("moved step %q to position %d\n", stepID, *to)
	return 0
}

// ---------------------------------------------------------------------------
// validate
// ---------------------------------------------------------------------------

func cmdValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}

	refs := fs.Args()
	if len(refs) == 0 {
		listing, err := st.List()
		if err != nil {
			return fail(err)
		}
		for _, e := range listing.Entries {
			fmt.Printf("ok   %s (%d steps)\n", e.Path, len(e.Book.Steps))
		}
		for _, p := range listing.Problems {
			fmt.Fprintf(os.Stderr, "fail %s\n", p.Error)
		}
		if len(listing.Problems) > 0 {
			return 1
		}
		fmt.Printf("\n%d book(s) valid\n", len(listing.Entries))
		return 0
	}

	failed := false
	for _, ref := range refs {
		e, err := st.Resolve(ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fail %v\n", err)
			failed = true
			continue
		}
		fmt.Printf("ok   %s (%d steps)\n", e.Path, len(e.Book.Steps))
	}
	if failed {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dir := fs.String("dir", defaultDir(), "folder holding book JSON files")
	asJSON := fs.Bool("json", false, "print the run result as JSON instead of streaming output")
	dryRun := fs.Bool("dry-run", false, "report what would run without executing it")
	var only repeatedFlag
	fs.Var(&only, "step", "run only this step id (repeatable; steps stay in book order)")
	if err := parseArgs(fs, args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		return fail(fmt.Errorf("usage: drill run <book> [--step ID ...] [--dry-run] [--json]"))
	}

	st, err := newStore(*dir)
	if err != nil {
		return fail(err)
	}
	e, err := st.Resolve(fs.Arg(0))
	if err != nil {
		return fail(err)
	}

	runner := &engine.Runner{Dir: filepath.Dir(e.Path)}
	if !*asJSON {
		runner.Sink = humanSink()
	}

	res, err := runner.Run(context.Background(), e.Book, engine.Options{
		StepIDs: only,
		DryRun:  *dryRun,
	})
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		if code := printJSON(res); code != 0 {
			return code
		}
	}
	if !res.OK {
		return 1
	}
	return 0
}

func humanSink() func(engine.Event) {
	total := 0
	return func(ev engine.Event) {
		switch ev.Type {
		case "run-start":
			total = ev.Total
			if total == 0 {
				fmt.Println("this book has no steps")
			}
			if ev.DryRun && total > 0 {
				fmt.Printf("dry run: %d step(s)\n", total)
			}
		case "step-start":
			fmt.Printf("\n[%d/%d] %s\n", ev.Index+1, total, ev.Title)
			if ev.Body == "" {
				return
			}
			if ev.IsScript {
				fmt.Printf("        %s script, %d lines\n", ev.Shell, len(strings.Split(ev.Body, "\n")))
				return
			}
			for _, line := range strings.Split(strings.TrimRight(ev.Body, "\n"), "\n") {
				fmt.Printf("        $ %s\n", line)
			}
		case "output":
			prefix := "        | "
			if ev.Stream == "stderr" {
				prefix = "        ! "
			}
			fmt.Println(prefix + ev.Text)
		case "step-end":
			fmt.Printf("        %s (%s)\n", statusLabel(ev.Status), durationText(ev.DurationMS))
		case "run-end":
			if ev.OK {
				fmt.Printf("\nrun finished in %s\n", durationText(ev.DurationMS))
			} else {
				fmt.Printf("\nrun failed after %s\n", durationText(ev.DurationMS))
			}
		}
	}
}

func statusLabel(status string) string {
	switch status {
	case engine.StatusPassed:
		return "[ok]"
	case engine.StatusFailed:
		return "[FAILED]"
	case engine.StatusSkipped:
		return "[skipped]"
	case engine.StatusPreview:
		return "[dry run]"
	default:
		return "[" + status + "]"
	}
}

func durationText(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func resolveScript(script, scriptFile string) (string, error) {
	if scriptFile != "" {
		data, err := os.ReadFile(scriptFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if script == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return script, nil
}

func humanize(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	if len(words) == 0 {
		return id
	}
	return strings.Join(words, " ")
}
