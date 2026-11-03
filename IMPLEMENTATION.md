# Drill — Implementation Document

> Status: draft v0.3 · Name: **Drill** · Desktop: Wails v2.12

---

## 1. What this is

**Drill** is a small, local-first tool for writing down a manual process and then
actually running it.

- A **Book** is one process: a release runbook, an onboarding checklist, a
  recovery procedure.
- A Book holds an **ordered list of Steps**. Each Step has a title, a markdown
  description, and *optionally* a shell command or an inline script.
- Steps can be executed on the spot, capturing stdout, stderr and the exit code.
- Steps can be **ticked off** as you work through a book, with one button to
  reset a book for a fresh run.
- A Book is **one plain JSON file**, human-readable and diffable, so a runbook
  can live in git next to the thing it describes.
- There are two front ends over **one** engine: a **desktop app** (native window)
  and a **`drill` CLI** for scripts, CI, and AI agents.

### Goals

| Goal | Meaning here |
| --- | --- |
| Executable documentation | The description and the command live in the same record; neither drifts from the other. |
| Git-native | One file per book, stable field order, 2-space indent, no timestamps or IDs that churn on save. |
| Zero infrastructure | No server, no database, no daemon, no account. Runs on a laptop. |
| One engine, two faces | GUI and CLI call the same `engine.Run` and produce identical results. |
| Cross-platform | Linux, macOS, Windows. |

### Non-goals (deliberately out of scope)

- Scheduling, retries, parallelism, or DAG dependencies between steps. Books are
  linear on purpose.
- Remote execution, SSH, containers, or a hosted service.
- User accounts, permissions, or an audit database.
- Persisting a history of past runs (the run result is returned, not stored).
- Templating/parameterisation of commands (`{{var}}`). Not yet; see §15.

---

## 2. Technology decision

**Go 1.23+ for everything, with Wails v2 as the desktop shell.**

The engine, the file format, the CLI, and the desktop backend are one Go module.
The UI is plain HTML/CSS/JS served from the embedded filesystem — no Node, no
npm install, no bundler.

### Why

- **One language, one binary per front end.** The CLI is a ~3 MB static binary;
  the desktop app is a ~9 MB binary with the webview toolkit linked in. No
  runtime to install.
- **`os/exec` is the right primitive.** Process groups, pipes, timeouts and exit
  status are exactly what a step runner needs, and Go's standard library covers
  them on all three OSes.
- **Wails gives a real desktop window** (WebKitGTK on Linux, WebView2 on
  Windows, WKWebView on macOS) without shipping a browser. The alternative —
  serving `localhost` and opening a tab — was rejected: it is a web app wearing
  a desktop costume, with a port to manage and a URL for the user to babysit.
- **The UI stack stays boring.** Plain HTML/CSS/JS with a 60-line markdown
  renderer. No framework, no bundler, no lockfile churn.

### Alternatives considered

| Option | Verdict |
| --- | --- |
| **Electron** | Rejected. 100+ MB runtime, a second language, and the Go engine would need an IPC bridge. |
| **Tauri** | Rejected for this repo. Excellent fit technically, but requires a Rust toolchain in addition to Go — two build systems for one small app. |
| **Fyne / Gio (pure Go GUI)** | Rejected. Immediate-mode custom widgets would mean re-implementing markdown rendering, selectable text and scrolling; the webview gives those for free. |
| **.NET + Avalonia** | Rejected. Would duplicate the engine in C# or force an out-of-process bridge; heavier deployment. |
| **HTTP server + browser tab** | Rejected as the primary UX (see above). The engine still supports the transport if a `drill serve` command is ever wanted. |

### Build matrix

| Target | Command | Output |
| --- | --- | --- |
| CLI, any OS | `go build -o drill ./cmd/drill` | `drill` (`.exe`) |
| Desktop, Linux | `wails build` | `build/bin/Drill` |
| Desktop, Windows | `wails build` (on Windows) | `build/bin/Drill.exe` |
| Desktop, macOS | `wails build -platform darwin/universal` (on macOS) | `build/bin/Drill.app` |

Wails does not cross-compile a GUI: each OS builds its own app. The CLI does
cross-compile trivially (`GOOS=windows go build ./cmd/drill`).

> Naming note: the product is **Drill**; the CLI binary is `drill` and the GUI is
> `Drill` (capitalised, as desktop apps convention). Install them into different
> directories on Windows — `drill.exe` and `Drill.exe` are the same file name on
> a case-insensitive filesystem.

---

## 3. Architecture

```
┌──────────────────────────┐           ┌──────────────────────────┐
│  Desktop app (Wails)     │           │  drill (CLI)             │
│  WebKitGTK / WebView2    │           │  flags + stdout/stderr   │
│  frontend/dist/*         │           │  cmd/drill/main.go       │
└───────────┬──────────────┘           └───────────┬──────────────┘
            │ window.go.main.App.*                  │ direct calls
            ▼                                       ▼
    ┌───────────────────┐                ┌────────────────────┐
    │ app.go            │                │ cmd/drill          │
    │ (bindings, config)│                │ (flag parsing)     │
    └─────────┬─────────┘                └─────────┬──────────┘
              └───────────────┬────────────────────┘
                              ▼
        ┌──────────────────────────────────────────────┐
        │ internal/engine   execute, stream, time out  │
        ├──────────────────────────────────────────────┤
        │ internal/progress checkmarks (outside books) │
        ├──────────────────────────────────────────────┤
        │ internal/store    folder of books, resolve   │
        ├──────────────────────────────────────────────┤
        │ internal/book     schema, validate, save     │
        └──────────────────────────────────────────────┘
                              ▼
                 <folder>/*.json  +  <config>/drill/progress.json
```

Two rules keep the split honest:

1. **`app.go` contains no business logic.** It resolves a book, calls the engine,
   and marshals results. Anything smart belongs in `internal/`.
2. **The CLI does not import `main`.** It calls `store`, `progress` and `engine`
   directly, so the two front ends cannot drift.

### Repository layout

```
.
├── main.go                  Wails entry point: window options, embed frontend
├── app.go                   Bindings exposed to JS + desktop state
├── config.go                Remember the last books folder
├── wails.json               Wails build configuration
├── cmd/drill/main.go        CLI (list/show/new/add/update/remove/move/validate/run)
├── internal/book/book.go    Schema, validation, atomic persistence
├── internal/store/store.go  Directory scanning, id/path resolution, create/delete
├── internal/progress/       Checkmarks, kept outside the book files
├── internal/engine/engine.go        Runner: process, pipes, timeouts, events
├── internal/engine/sys_unix.go      Process-group kill (POSIX)
├── internal/engine/sys_windows.go   Process-tree kill (taskkill /T)
├── frontend/dist/           index.html, app.js, style.css (embedded, no build)
├── build/                   Icons, Windows manifest, macOS plist (Wails assets)
├── tools/icon/main.go       Regenerates build/appicon.png and windows/icon.ico
└── examples/                Sample books
```

---

## 4. Book format

One file per book, `<id>.json`. Written with `json.MarshalIndent` (2 spaces),
`SetEscapeHTML(false)` so `>` and `&` in commands stay readable, and a trailing
newline. Files are replaced atomically (temp file + rename), so an interrupted
save can never truncate a runbook.

### Example

```json
{
  "version": 1,
  "id": "release-1-4-0",
  "title": "Release 1.4.0",
  "description": "Cut, verify and publish the **1.4.0** release.",
  "tags": ["release"],
  "defaults": {
    "shell": "bash",
    "timeoutSeconds": 600
  },
  "steps": [
    {
      "id": "run-tests",
      "title": "Run the test suite",
      "description": "Everything must be green before tagging.",
      "command": "go test ./..."
    },
    {
      "id": "build",
      "title": "Build the binaries",
      "description": "Cross-compile for linux, darwin and windows.",
      "script": "set -euo pipefail\nfor os in linux darwin windows; do\n  GOOS=$os go build ./cmd/drill\ndone",
      "timeoutSeconds": 900
    },
    {
      "id": "announce",
      "title": "Post the release notes",
      "description": "Copy the changelog into `#releases`. No command: this step is a checklist item."
    }
  ]
}
```

### Fields

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `version` | int | yes | Must be `1`. |
| `id` | string | yes | `[a-z0-9][a-z0-9_-]*`, ≤ 64 chars. Becomes the file name (`<id>.json`); dots are excluded so `foo.bar` cannot produce `foo.bar.json.json` ambiguity. |
| `title` | string | yes | Human label. |
| `description` | string | no | Markdown. |
| `tags` | string[] | no | Free-form, for filtering and for humans. |
| `defaults.cwd` | string | no | Base directory for steps, relative to the book file. |
| `defaults.shell` | string | no | `sh`, `bash`, `pwsh`, `powershell`, `cmd`, `python`, `python3`. |
| `defaults.timeoutSeconds` | int | no | Applied when a step does not set one. `0`/absent means the 10-minute engine default. |
| `steps[].id` | string | yes | Unique within the book, same charset as the book id. |
| `steps[].title` | string | yes | |
| `steps[].description` | string | no | Markdown. |
| `steps[].command` | string | one of | Run through the shell, so pipes and redirection work. |
| `steps[].script` | string | one of | Multi-line body written to a temp file and executed by the interpreter. |
| `steps[].shell` | string | no | Overrides `defaults.shell`. |
| `steps[].cwd` | string | no | Overrides `defaults.cwd`. |
| `steps[].timeoutSeconds` | int | no | Overrides `defaults.timeoutSeconds`. |
| `steps[].continueOnError` | bool | no | If true, a failure does not stop the book. |

Field order in the struct (and therefore in the file): `version, id, title,
description, tags, defaults{cwd, shell, timeoutSeconds}, steps[]`.

A step may have **neither** `command` nor `script` (a pure checklist item), but
not both.

### Validation

`book.Validate` collects **every** problem and returns them joined by newlines,
with the offending index and field, e.g.:

```
steps[2].id: duplicate of steps[0] ("build")
steps[2]: has both command and script; pick one
defaults.shell: unknown shell "fish"
```

Unknown JSON fields are **rejected** (`DisallowUnknownFields`). This turns a
typo like `"comand"` into an error instead of a silently unrunnable step. The
cost is that additive schema changes need a `version` bump.

Loading a file validates it; the store records invalid files as *problems* and
keeps listing the rest, so one broken book never hides the others.

---

## 5. Execution semantics

Resolution order, most specific wins:

| Setting | Step → Book default → Fallback |
| --- | --- |
| Shell | `step.shell` → `defaults.shell` → `sh` (POSIX) / `pwsh`, else `cmd` (Windows) |
| Working dir | `step.cwd` → `defaults.cwd` → the book's directory. Relative values resolve against the book's directory. |
| Timeout | `step.timeoutSeconds` → `defaults.timeoutSeconds` → 600 s |

### Invocation

| Shell | Inline command | Script file |
| --- | --- | --- |
| `sh`, `bash` | `sh -c <command>` | `sh /tmp/drill-*.sh` |
| `pwsh`, `powershell` | `pwsh -NoProfile -NonInteractive -Command <command>` | `... -File /tmp/drill-*.ps1` |
| `cmd` | `cmd /C <command>` | `cmd /C /tmp/drill-*.cmd` |
| `python`, `python3` | `python -c <command>` | `python /tmp/drill-*.py` |

Script bodies are written to a system temp file with the right extension and
removed after the run. The child process inherits the parent's environment; no
environment variables are set by the format (see §15).

### Streaming and capture

stdout and stderr are read through two `os.Pipe` read ends, line by line, by a
`bufio.Scanner` (64 KB initial buffer, 4 MB max line). Each line is

1. appended to a per-step buffer (`StepResult.Stdout` / `.Stderr`), and
2. emitted as an `output` event with `stream: "stdout" | "stderr"`.

Reading raw pipes instead of `cmd.StdoutPipe()` avoids `cmd.Wait()` closing the
pipe while the readers are still draining, which silently truncates output in
the common pattern. Both readers are joined before `cmd.Wait()` is called, and
the write ends are closed in the parent right after `Start`, so EOF arrives when
the last descendant exits.

### Timeout and cancellation

- A `context.WithTimeout` is derived from the caller's context.
- A watchdog goroutine calls `killTree` when that context is done.
- POSIX: the child is started with `Setpgid`, and the group is killed with
  `kill(-pid, SIGKILL)`, so `sh -c "... | tee ..."` and its children die too.
- Windows: `taskkill /T /F /PID <pid>` walks the tree, falling back to
  `Process.Kill`.
- A timed-out step is `failed` with `timedOut: true` and the message
  `timed out after 10m0s`.
- A cancelled run (Stop button, window close) is `failed` with `error: "canceled"`.

### Failure policy

Fail-fast is the default: the first failing step halts the book and the
remaining steps are reported as `skipped`. `continueOnError: true` on the failing
step lets the run continue. A step with a non-zero exit code is a failure even
if it produced no output.

### Dry run

`--dry-run` / the **Dry run** button resolves shells, working directories and
timeouts, emits `step-start` and `step-end` with status `preview`, and executes
nothing. It is the cheapest way to check a runbook after editing it.

### Result shape

```json
{
  "bookId": "release-1-4-0",
  "dryRun": false,
  "ok": false,
  "startedAt": "2026-11-02T21:00:00Z",
  "durationMs": 4213,
  "steps": [
    {
      "index": 0, "stepId": "run-tests", "title": "Run the test suite",
      "status": "passed", "shell": "bash", "exitCode": 0,
      "durationMs": 3900, "stdout": "ok  ...\n"
    },
    {
      "index": 1, "stepId": "build", "title": "Build the binaries",
      "status": "failed", "shell": "bash", "exitCode": 2,
      "timedOut": false, "durationMs": 110, "stderr": "make: *** ...\n"
    },
    { "index": 2, "stepId": "announce", "title": "Post the release notes", "status": "skipped", "durationMs": 0 }
  ]
}
```

Statuses: `passed`, `failed`, `skipped`, `preview`.

---

## 6. Desktop app

### Shell

| Option | Value |
| --- | --- |
| Title | `Drill` (replaced at runtime with `Drill — <book title>`) |
| Size | 1200 × 820, minimum 880 × 560 |
| Background | `#0f1116` (matches the UI so there is no white flash) |
| Assets | `//go:embed all:frontend/dist` |
| Bindings | `[]interface{}{app}` |

`OnStartup` resolves the books folder, opens the progress store and keeps the
Wails context; `OnBeforeClose` cancels a running step so closing the window does
not orphan a child process.

### Books folder resolution (startup)

1. `DRILL_DIR` environment variable.
2. The last folder chosen in the app (`<UserConfigDir>/drill/config.json`).
3. The current working directory, if it already contains a file that **loads as
   a Book** (not merely any `*.json` — a repo's `wails.json` must not qualify).
4. `~/Drill` (created on demand).

**Change folder** opens the native directory picker and persists the choice.

### Go ↔ JS bindings

All methods are called as `window.go.main.App.<Method>(...)` and return a
Promise. Errors reject with the Go error message, which the UI shows in a toast.

| Method | Signature | Notes |
| --- | --- | --- |
| `ListBooks` | `() (Listing, error)` | Books + problems + folder. |
| `GetBook` | `(id string) (*book.Book, error)` | |
| `CreateBook` | `(book.Book) (*book.Book, error)` | Fills `version`/`id` when omitted. |
| `UpdateBook` | `(book.Book) (*book.Book, error)` | Writes back to the file it came from. |
| `DeleteBook` | `(id string) error` | Removes the file and its checkmarks. |
| `AddStep` | `(bookID string, step book.Step) (*book.Book, error)` | Appends; derives an id from the title. |
| `UpdateStep` | `(bookID, stepID string, step book.Step) (*book.Book, error)` | Replaces in place. |
| `DeleteStep` | `(bookID, stepID string) (*book.Book, error)` | |
| `MoveStep` | `(bookID, stepID string, to int) (*book.Book, error)` | `to` is a 0-based index. |
| `RunBook` | `(bookID string, stepIDs []string, dryRun bool) (*engine.RunResult, error)` | Streams events; resolves when the run ends. |
| `StopRun` | `()` | Cancels the run and kills the process tree. |
| `ChooseFolder` | `() (string, error)` | Native picker; empty string means cancelled. |
| `BooksDir` | `() string` | |
| `CheckedSteps` | `(bookID string) ([]string, error)` | Ticked step ids, filtered to steps that still exist. |
| `SetStepChecked` | `(bookID, stepID string, checked bool) ([]string, error)` | Returns the book's checked set afterwards. |
| `ResetProgress` | `(bookID string) ([]string, error)` | Clears every tick for the book ("fresh start"). |

### Checkmarks

Every step carries a checkbox; the book header shows `done/total` and a **Reset**
button that clears them all for the next run.

State lives **outside** the book, in `<UserConfigDir>/drill/progress.json`:

```json
{
  "version": 1,
  "books": {
    "/home/you/runbooks/release-1-4-0.json": {
      "checked": ["check-tree", "run-tests"],
      "updatedAt": "2026-11-02T22:18:03Z"
    }
  }
}
```

Why not in the book: a Book is a git document. If ticks lived there, every tick
would dirty the working tree, two people running the same release would conflict
on the same lines, and `git diff` would stop answering "what changed in the
procedure?". The file stays byte-identical while it is worked through. The cost
is that progress is local to the machine — the right trade for a runbook.

Details worth knowing:

- Keyed by the book's absolute path, so two folders may hold books with the
  same id.
- Ids for deleted steps are ignored on read (the counter can never exceed the
  step count) and disappear on the next reset.
- A missing or corrupt progress file means "nothing ticked" and never blocks the
  app; the next write repairs it atomically.
- Deleting a book deletes its progress entry.
- Ticking a step does not run it, and running a step does not tick it: they are
  deliberately independent signals ("I did this" vs "the command exited 0").
- Ticking updates the DOM in place, so it never disturbs streaming output.

### Run event stream

`RunBook` emits `run:event` through `runtime.EventsEmit`; the UI subscribes once
with `runtime.EventsOn("run:event", ...)`.

| Event | Fields | UI reaction |
| --- | --- | --- |
| `run-start` | `bookId`, `title`, `total`, `dryRun` | Resets result panes. |
| `step-start` | `index`, `stepId`, `title`, `shell`, `body`, `isScript` | Marks the card running, clears its output. |
| `output` | `stepId`, `stream`, `text` | Appends a line (stderr in red). |
| `step-end` | `stepId`, `status`, `exitCode`, `timedOut`, `durationMs`, `error` | Shows a status pill and duration. |
| `run-end` | `ok`, `durationMs` | Toast: finished / stopped. |

### UI structure

`frontend/dist` — three files, no build step:

- `index.html` — sidebar (book list, folder controls) and main pane.
- `app.js` — state, rendering (template strings), a ~60-line markdown renderer
  (headings, lists, code fences, inline code, bold/italic, links, with URL
  scheme filtering), the run event handler, checkbox/reset handling, and one
  delegated click/submit/change/keydown handler.
- `style.css` — dark theme, system font stack, responsive below 760 px.

Deliberate choices:

- **No framework and no re-render during a run.** Results and checkboxes are
  applied to the existing DOM, so streaming output is not lost and focus is not
  stolen.
- **Text is escaped before markdown is applied**; link targets are restricted to
  `http(s)`, `mailto`, absolute-path and relative forms.
- **Unsaved forms** are kept in `state` and re-rendered on demand; `Esc`
  cancels an edit.

---

## 7. CLI

```
drill <command> [flags] [args]
```

| Command | Purpose | Example |
| --- | --- | --- |
| `list` (`ls`) | Table or JSON of books | `drill list --dir runbooks` |
| `show` (`cat`) | Print one book as JSON | `drill show release-1-4-0` |
| `new` | Create an empty book | `drill new release-1-4-0 --title "Release 1.4.0"` |
| `add` | Append/insert a step | `drill add rel --title "Tests" --command "go test ./..."` |
| `update` (`set`) | Change step fields | `drill update rel run-tests --timeout 900` |
| `remove` (`rm`) | Delete a step | `drill remove rel run-tests` |
| `move` (`mv`) | Reorder | `drill move rel run-tests --to 1` |
| `validate` (`check`) | Validate one book or the folder | `drill validate --dir runbooks` |
| `run` | Execute | `drill run rel --step build --json` |

Conventions:

- `<book>` is either a book id or a path to a `.json` file.
- `--dir` defaults to `$DRILL_DIR`, else `.`.
- Flags may appear before or after positional arguments (`drill add demo --title X`
  and `drill add --title X demo` are equivalent). Go's `flag` package does not do
  this itself; `parseArgs` hoists flags (using `IsBoolFlag` so a bool flag does
  not swallow the next argument) and keeps `--` meaningful.
- Flags only change what they name: `drill update` uses `FlagSet.Visit` to tell
  "not set" from "set to empty".
- `--command` clears `script` and vice versa, because "set the payload" is what
  the user means.
- `--script -` reads the body from stdin, which is how an AI agent or CI job
  authors a multi-line script without shell quoting:
  `printf 'set -e\nmake\n' | drill add rel --title Build --script -`.
- `--json` on `list` and `run` prints machine-readable output with no log noise.

Exit codes: `0` success, `1` failure (run failed, invalid book, I/O error),
`2` usage error.

`run` without `--json` streams human-readable output:

```
[1/3] Run the test suite
        $ go test ./...
        | ok  github.com/kingzad/drill/internal/book  0.011s
        [ok] (1.2s)
```

---

## 8. Error handling

| Situation | Behaviour |
| --- | --- |
| Book JSON is malformed / unknown field | `book.Load` wraps the path and the decoder error; `list` reports it as a problem, `show`/`run` exit 1. |
| Book fails validation | All problems are listed at once, with `steps[i].field` prefixes. |
| Step id not found | `unknown step id(s): a, b` (sorted) before anything runs. |
| Working directory missing | Step fails with `working directory: stat ...: no such file or directory`; the run continues per failure policy. |
| Interpreter missing (`bash` not installed) | Step fails with `could not start bash: exec: "bash": executable file not found in $PATH`. |
| Non-zero exit | `failed` + `exitCode`. |
| Timeout | `failed` + `timedOut: true`; whole process tree killed. |
| Cancel | `failed` + `error: "canceled"`; remaining steps `skipped`. |
| Save fails (read-only file, permissions) | Error surfaces to the toast / stderr; the in-memory book is unchanged. |
| Progress file unwritable | Ticking fails with a toast; the runbook itself is unaffected. |

The engine returns an `error` only when the run cannot start (bad selection).
Per-step failures are data, not errors, so a CI job can inspect partial results.

---

## 9. Security

The product's whole point is executing arbitrary commands written by the user,
so the threat model is narrow but explicit:

- **No network listener.** The desktop app embeds the UI and talks over the
  Wails IPC bridge; there is no port to scan or bind.
- **No remote content.** The UI loads only embedded assets. Links from markdown
  are filtered to `http(s)`, `mailto`, absolute-path and relative forms; scripts
  cannot be injected because all text is escaped before formatting.
- **Path safety.** Book and step ids are validated against
  `^[a-z0-9][a-z0-9_-]*$` before they are used to build a file path, so an id
  like `../../.ssh/authorized_keys` cannot escape the books folder.
- **Temp scripts** are created with `os.CreateTemp` (mode 0600) and removed
  after the run.
- **Permissions.** Books are written atomically as 0644 through a temp file in
  the same directory. The progress file holds step ids only — no output, no
  secrets, no timestamps beyond `updatedAt`.
- A book is exactly as dangerous as the shell the user already has; do not run
  untrusted books, the same rule as `Makefile`s.

---

## 10. Cross-platform notes

| Concern | Handling |
| --- | --- |
| Line endings | The engine normalises nothing; `\r\n` is stripped by the markdown renderer only. JSON output always uses `\n`. |
| Shells | `sh`/`bash` exist on macOS and most Linux; Windows falls back to `pwsh` when installed, else `cmd`. Runbooks that must be portable should set `"shell": "pwsh"` (or `python`) explicitly. |
| Paths | Everything goes through `filepath`, never string concatenation. `cwd` may be absolute or relative to the book. |
| Process trees | POSIX process groups vs `taskkill /T` on Windows (see §5). |
| Atomic rename | `os.Rename` overwrites on both platforms (MoveFileEx on Windows). |
| Config locations | `os.UserConfigDir()`: `~/.config/drill/` (Linux), `~/Library/Application Support/drill/` (macOS), `%AppData%\drill\` (Windows). Holds `config.json` and `progress.json`. |
| Console output | The CLI uses ASCII markers (`[ok]`, `[FAILED]`) and no ANSI colour, so `cmd.exe` and CI logs stay readable. |

---

## 11. Testing strategy

| Layer | What is tested | How |
| --- | --- | --- |
| `book` | Validation matrix (every rule + "all problems at once"), slug/id rules, unknown-field rejection, round-trip equality, 2-space indent + no HTML escaping, atomic save leaves exactly one file at 0644, description-only steps accepted | Table-driven unit tests, `t.TempDir()` |
| `engine` | stdout/stderr/exit-code capture, inline script execution, timeout kills the process *tree*, fail-fast skipping, `continueOnError`, dry run executes nothing, unknown/duplicate step ids, book order preserved when selecting, `cwd` override, missing cwd, shell override, unknown shell, event sequence, timeout precedence, per-shell invocation specs | Integration tests with `sh` (skipped on Windows) |
| `store` | Sorted listing, invalid files reported as problems without aborting, `*.txt` ignored, duplicate create refused, resolve by id / by path / after a rename, save-back, delete | `t.TempDir()` |
| `progress` | Set/unset/reset, per-book isolation, persistence across a restart, unchecking the last step persists, corrupt file tolerated, atomic write | `t.TempDir()` |
| CLI | Flags after positionals (the bug that made `drill add book --title X` ignore X), bool flags not eating positionals, `--` terminator, unknown flag, exit codes, end-to-end create→add→update→move→run→validate, `--json` parses as one document | Unit tests plus a full pass through `run([]string) int` |
| Desktop | `App` bindings against a temp folder: create, add/update/move/delete steps, duplicate and out-of-range rejection; checkmarks incl. deleted-step filtering and reset-on-delete | Go tests, no webview needed |

Current state: `go test ./...` covers all six packages (~50 cases). The engine
tests are the slow ones (~1 s, dominated by the timeout case). The UI itself is
still verified by hand (M9.3–M9.6, M13.7 in the task plan).

Bugs the first test run caught, all fixed:

1. **CLI flags after a positional were silently ignored.** Go's `flag` stops at
   the first non-flag argument, so `drill add demo --title "Tests"` treated
   `--title "Tests"` as positional arguments and failed with "title is
   required". Fixed with `parseArgs`, which hoists flags and their values ahead
   of the positionals (and keeps `--` meaningful).
2. **Description-only steps were rejected** by a validation branch that
   contradicted the documented behaviour. Removed; a step may have neither
   payload.
3. **Ids allowed dots**, so a slug like `foo.bar` produced `foo.bar.json.json`
   ambiguity. Dots are now stripped from slugs and rejected by `ValidID`.
4. **`containsBooks` treated any `*.json` as a book**, so opening the app in a
   repo containing `wails.json` or `package.json` would pick the wrong folder.
   It now requires a file that actually loads as a Book.

---

## 12. Build and packaging

```bash
# toolchain
go build ./... && go vet ./...

# CLI
go build -trimpath -ldflags "-s -w" -o drill ./cmd/drill

# desktop (per OS)
wails build            # Linux: needs libgtk-3-dev + libwebkit2gtk-4.0/4.1-dev
wails build -tags webkit2_41   # only when the system has webkit2gtk-4.1 and no 4.0
```

### Icon

The icon is **drawn in Go** (`tools/icon`), not exported from a design tool, so
it can be regenerated and reviewed in a diff:

```bash
go run ./tools/icon
```

| Output | Purpose |
| --- | --- |
| `build/appicon.png` (1024²) | Linux window icon (embedded via `//go:embed`), macOS `.icns` source (Wails generates it), Windows `.ico` source |
| `build/windows/icon.ico` | Six frames: 16, 32, 48, 64, 128, 256 (PNG-compressed frames, Vista+) |
| `docs/icon/*.png` (512²) | Design candidates kept for review |

The chosen mark is the **checklist**: three rows, two ticked (the second one
accent-coloured), the third an open outline bar — "work through the steps, one
at a time". The two alternates stay in `docs/icon/` for comparison, and
`const productionVariant` at the top of `tools/icon/main.go` selects which one is
shipped (1 = list + play, 2 = checklist, 3 = check on an accent tile).

Linux note: `linux.Options.Icon` sets `WM_HINTS`/`_NET_WM_ICON` on the window, but
modern docks and app grids resolve the icon from the `.desktop` entry instead, so
a `.desktop` file plus a hicolor PNG is still the right way to ship it (see §12).

Windows manifest and version info live in `build/windows/`, the macOS
`Info.plist` in `build/darwin/`.

Not done yet: installers (`.deb`/`.dmg`/MSI), code signing, notarisation, and CI
that builds all three platforms. The CLI needs none of that.

---

## 13. Design decisions worth remembering

| Decision | Reason |
| --- | --- |
| Step ids are stable, human-written strings, not UUIDs | They show up in diffs, CLI arguments and event payloads; a rename is a deliberate edit. |
| `continueOnError` lives on the *failing* step | Reads as "this step is allowed to fail and we carry on", which is how people describe the procedure. |
| Fail-fast by default | A runbook that keeps going after a failure is how you get half-deployed releases. |
| Checkmarks live outside the book | A tick is run state, not part of the procedure; keeping it out keeps `git diff` meaningful and avoids merge conflicts mid-release. |
| No run history on disk | It would add churn to the git repo and a cleanup problem. The result is returned to whoever ran it; tee it if you need a record. |
| Description-only steps are valid | Onboarding checklists are mostly reading and doing, not executing. |
| Strict JSON decoding | A typo in `command` should be an error, not a silent no-op step. |
| Embedded UI, no bundler | `wails build` works on a machine with Go and GTK, nothing else. |
| Ticks and runs are independent | "I did this" and "the command exited 0" are different facts; merging them would make either signal unreliable. |

---

## 14. Naming

The product is **Drill**: a drill is a rehearsed procedure, and running a drill
is routine. That is exactly the promise — write the procedure down, then run it —
and it covers the incident-recovery ("fire drill") case the format was designed
around.

Rejected: **Steps** (generic, unsearchable, collides with AWS Step Functions and
GitHub Actions *steps*, reads poorly as a command), **Rung**, **Baton**,
**Stepwise**, **Routine** (all workable; see the chat shortlist).

Naming rules this project follows:

- CLI binary: `drill` — one word, easy to type in scripts and agent prompts.
- GUI: `Drill` — capitalised per desktop convention, shown in the window title,
  the dock/taskbar and the bundle name.
- The Wails binary and the CLI binary must not be installed into the same
  directory on Windows (case-insensitive filesystem).
- Env var: `DRILL_DIR`. Config: `<UserConfigDir>/drill/config.json`. Progress:
  `<UserConfigDir>/drill/progress.json`.

---

## 15. Future work (explicitly not in v0.1)

- `drill serve` — the HTTP/NDJSON transport that existed before the desktop
  conversion, for browser use and remote agents.
- Variables and secrets (`{{target}}`), with prompting and redaction in output.
- `drill doctor` — which shells exist, whether `cwd` resolves, permissions.
- Run reports written next to the book (`<id>.run.json`) behind a flag.
- Per-step environment variables and `stdin` injection.
- Book imports (a step that runs another book), and conditional steps.
- Optional opt-in per-book progress in git (a "shared" mode) for teams that want
  ticks committed as evidence. Deliberately not the default.
