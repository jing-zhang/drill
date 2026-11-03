package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kingzad/drill/internal/book"
	"github.com/kingzad/drill/internal/engine"
)

// TestParseArgsFlagsAfterPositionals is the regression test for the bug where
// `drill add my-book --title "Tests"` silently ignored every flag, because
// flag.Parse stops at the first positional argument.
func TestParseArgsFlagsAfterPositionals(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	title := fs.String("title", "", "")
	force := fs.Bool("force", false, "")
	dir := fs.String("dir", ".", "")

	args := []string{"my-book", "--title", "Run tests", "--force", "--dir=/tmp/books", "extra"}
	if err := parseArgs(fs, args); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}

	if *title != "Run tests" {
		t.Errorf("title = %q", *title)
	}
	if !*force {
		t.Error("force should be true")
	}
	if *dir != "/tmp/books" {
		t.Errorf("dir = %q", *dir)
	}
	if got := fs.Args(); !reflect.DeepEqual(got, []string{"my-book", "extra"}) {
		t.Errorf("positional args = %v", got)
	}
}

func TestParseArgsBoolDoesNotEatPositional(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "")

	if err := parseArgs(fs, []string{"dry-run", "--dry-run"}); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !*dry {
		t.Error("dry-run should be true")
	}
	if got := fs.Args(); !reflect.DeepEqual(got, []string{"dry-run"}) {
		t.Errorf("positional args = %v", got)
	}
}

func TestParseArgsDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	title := fs.String("title", "", "")

	if err := parseArgs(fs, []string{"--title", "T", "--", "--not-a-flag"}); err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if *title != "T" {
		t.Errorf("title = %q", *title)
	}
	if got := fs.Args(); !reflect.DeepEqual(got, []string{"--not-a-flag"}) {
		t.Errorf("positional args = %v", got)
	}
}

func TestParseArgsRejectsUnknownFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := parseArgs(fs, []string{"--nope"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
}

func TestVersionCommand(t *testing.T) {
	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version exit code = %d", code)
	}
}

func TestUnknownCommand(t *testing.T) {
	if code := run([]string{"definitely-not-a-command"}); code != 2 {
		t.Errorf("unknown command exit code = %d, want 2", code)
	}
}

// capture runs fn with stdout redirected and returns what it printed.
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	code := fn()
	w.Close()
	os.Stdout = old
	<-done
	return buf.String(), code
}

func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, code := capture(t, func() int { return run(args) })
	if code != 0 {
		t.Fatalf("drill %v exited %d:\n%s", args, code, out)
	}
	return out
}

// TestCLIEndToEnd exercises the real command paths against a temp folder:
// create, add, edit, reorder, run with --json, and validate.
func TestCLIEndToEnd(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DRILL_DIR", dir)

	mustRun(t, "new", "demo", "--title", "Demo book")
	mustRun(t, "add", "demo", "--title", "Say hi", "--command", "echo hi")

	load := func() *book.Book {
		t.Helper()
		b, err := book.Load(filepath.Join(dir, "demo.json"))
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return b
	}

	if step := load().Steps[0]; step.Command != "echo hi" || step.ID != "say-hi" {
		t.Fatalf("step = %+v", step)
	}

	// --command and --script are mutually exclusive fields: setting one clears
	// the other, because that is what the user means.
	mustRun(t, "update", "demo", "say-hi", "--script", "echo changed")
	step := load().Steps[0]
	if step.Script != "echo changed" || step.Command != "" {
		t.Fatalf("script update = %+v", step)
	}

	// A flag set explicitly to the empty string must actually clear the field.
	mustRun(t, "update", "demo", "say-hi", "--shell", "")
	if got := load().Steps[0].Shell; got != "" {
		t.Fatalf("shell = %q, want empty", got)
	}

	// --json must emit exactly one JSON document and nothing else.
	out := mustRun(t, "run", "demo", "--json")
	var res engine.RunResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("--json is not valid JSON (%v):\n%s", err, out)
	}
	if len(res.Steps) != 1 || res.Steps[0].Status != engine.StatusPassed {
		t.Fatalf("result = %+v", res)
	}
	if res.Steps[0].Stdout != "changed\n" {
		t.Fatalf("stdout = %q", res.Steps[0].Stdout)
	}

	// Reordering and validation through the CLI.
	mustRun(t, "add", "demo", "--title", "Second", "--command", "true")
	mustRun(t, "move", "demo", "second", "--to", "1")
	if first := load().Steps[0]; first.ID != "second" {
		t.Fatalf("move did not apply: %s is first", first.ID)
	}
	mustRun(t, "validate", "demo")

	// A failing run must exit non-zero, and the JSON result must say so.
	mustRun(t, "add", "demo", "--title", "Boom", "--command", "exit 9")
	out, code := capture(t, func() int { return run([]string{"run", "demo", "--step", "boom", "--json"}) })
	if code != 1 {
		t.Fatalf("failing run exited %d, want 1", code)
	}
	var failed engine.RunResult
	if err := json.Unmarshal([]byte(out), &failed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if failed.OK {
		t.Error("OK should be false")
	}
	if failed.Steps[0].ExitCode == nil || *failed.Steps[0].ExitCode != 9 {
		t.Errorf("exit code = %v, want 9", failed.Steps[0].ExitCode)
	}
}
