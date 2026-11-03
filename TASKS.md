# Drill — Task Plan

> Companion to [`IMPLEMENTATION.md`](IMPLEMENTATION.md). Statuses reflect what
> was actually executed, not what was written.
>
> **Legend** — `DONE` verified by running the check · `BUILT` compiles/vets but
> the behaviour was not exercised · `TODO` not started · `BLOCKED` waiting on a
> decision or an environment.

---

## How to use this plan

1. Work top to bottom; later phases assume the earlier ones.
2. A task is done only when its **Check** command has been run and passed.
3. Update this file in the same commit as the code — status drift is worse than
   no plan.

### Definition of done (applies to every task)

- `go build ./... && go vet ./...` pass.
- New behaviour has a test (once `T6` exists).
- The relevant doc section in `IMPLEMENTATION.md` is accurate.
- No `.cache/`, `build/bin/`, or `frontend/wailsjs/` artefacts are committed.

---

## Phase overview

| Phase | Theme | Status |
| --- | --- | --- |
| M0 | Decisions and skeleton | DONE |
| M1 | Book model and persistence | DONE |
| M2 | Folder store | DONE |
| M3 | Execution engine | DONE |
| M4 | CLI | DONE |
| M5 | Desktop shell | DONE |
| M6 | Automated tests | DONE |
| M7 | Examples and README | IN PROGRESS |
| M8 | Repo hygiene | IN PROGRESS |
| M9 | Runtime verification | TODO |
| M10 | Packaging and CI | TODO |
| M11 | Rename to final product name | DONE |
| M13 | Step checkmarks | DONE |
| M14 | App icon | DONE |
| M12 | Backlog (post-v0.1) | TODO |

---

## M0 — Decisions and skeleton `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M0.1 | Choose Go + Wails v2 over Electron/Tauri/Fyne/.NET | DONE |
| M0.2 | Reject the HTTP-server-plus-browser design as the primary UX | DONE |
| M0.3 | Fix the JSON schema (`version: 1`) and validation rules | DONE |
| M0.4 | Fix the module layout (`internal/*` shared, two thin front ends) | DONE |
| M0.5 | Decide: one file per book, `<id>.json`, atomic saves | DONE |
| M0.6 | Confirm the toolchain on the target machine (Go 1.26, GTK3, WebKit2GTK 4.0/4.1, Wails 2.12) | DONE |

**Check:** `go build ./... && wails version`

---

## M1 — Book model and persistence `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M1.1 | `Book`/`Step`/`Defaults` structs with stable JSON field order | DONE |
| M1.2 | `Validate` collects all problems with `steps[i].field` context | DONE |
| M1.3 | Id rules (`[a-z0-9][a-z0-9._-]*`, ≤64) and `Slug` | DONE |
| M1.4 | Strict decode (`DisallowUnknownFields`) so typos fail loudly | DONE |
| M1.5 | Atomic save: temp file in the target dir + rename, 0644 | DONE |
| M1.6 | `SetEscapeHTML(false)` so `>`, `<`, `&` stay readable in commands | DONE |

**Check:** `go build ./... && go vet ./...`

---

## M2 — Folder store `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M2.1 | `List` scans `*.json`, skips dotfiles, sorts by id | DONE |
| M2.2 | Invalid files become `Problem`s instead of aborting the scan | DONE |
| M2.3 | `Resolve` accepts an id or a path, with a scan fallback | DONE |
| M2.4 | `Create` refuses to overwrite an existing file | DONE |
| M2.5 | `SaveTo` writes back to the file a book was loaded from | DONE |

**Check:** covered by M6.3

---

## M3 — Execution engine `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M3.1 | Shell dispatch: `sh`, `bash`, `pwsh`, `powershell`, `cmd`, `python` | DONE |
| M3.2 | Inline command via `-c`/`-Command`, script via temp file | DONE |
| M3.3 | Line-streamed stdout/stderr over `os.Pipe` + event sink | DONE |
| M3.4 | Capture exit code, stdout, stderr, duration per step | DONE |
| M3.5 | Timeout with process-group kill (POSIX) and `taskkill /T` (Windows) | DONE |
| M3.6 | Fail-fast, `continueOnError`, skipped remainder, cancellation | DONE |
| M3.7 | Step selection by id, validated before anything runs | DONE |
| M3.8 | Dry run (`preview`) that resolves shell/cwd/timeout | DONE |
| M3.9 | `cwd`/`shell`/`timeout` resolution: step → book → fallback | DONE |

**Check:** covered by M6.2 — this is the phase most in need of tests.

---

## M4 — CLI `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M4.1 | `list` (table + `--json`) | DONE |
| M4.2 | `show` prints canonical JSON | DONE |
| M4.3 | `new` with slugified id | DONE |
| M4.4 | `add` with `--command` / `--script` / `--script -` / `--script-file` / `--at` | DONE |
| M4.5 | `update` using `FlagSet.Visit` for true tri-state flags | DONE |
| M4.6 | `remove`, `move`, `validate` | DONE |
| M4.7 | `run` with `--step`, `--dry-run`, `--json`, human stream | DONE |
| M4.8 | Exit codes 0/1/2 | DONE |
| M4.9 | Smoke-test every command against a temp folder | DONE |

**Check:** `go build -o /tmp/drill ./cmd/drill && /tmp/drill version`

---

## M5 — Desktop shell `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M5.1 | Wails window, embedded `frontend/dist`, no npm | DONE |
| M5.2 | Bindings: book/step CRUD, reorder, run, stop, folder picker | DONE |
| M5.3 | `run:event` stream wired to the UI | DONE |
| M5.4 | Books-folder resolution + remembered folder in `UserConfigDir` | DONE |
| M5.5 | UI: sidebar, book header, step cards, inline editors, markdown, results | DONE |
| M5.6 | Generated icon set (PNG + ICO) | DONE |
| M5.7 | Launch the app and confirm the window renders | DONE |
| M5.8 | Click through: create book → add step → run → stop → delete | TODO |

**Check:** `wails build && ./build/bin/Drill`

> `wails build` succeeds and the window was verified on X11: title
> `Drill — Onboarding a new engineer`, 1200x820, process stable for 6 s. That
> proves the embedded assets loaded and `ListBooks`/`GetBook`/`WindowSetTitle`
> all worked. The click-through (M5.8, M9.3–M9.6) is still manual.

---

## M6 — Automated tests `DONE`

| ID | Task | Acceptance | Check |
| --- | --- | --- | --- |
| M6.1 | `book` validation matrix + round-trip + atomic save | Table test covers every validation error string; a save leaves exactly one file | `go test ./internal/book` |
| M6.2 | `engine` execution tests | stdout/stderr/exit code captured; timeout kills a `sleep` tree; fail-fast skips the rest; `continueOnError` continues; dry run executes nothing; unknown id errors | `go test ./internal/engine` |
| M6.3 | `store` tests | duplicate create fails; resolve by id and path; invalid file listed as a problem | `go test ./internal/store` |
| M6.4 | CLI flag semantics | `update --shell ""` clears; `--command` clears `script`; `--json` emits one JSON document; a failing run exits 1 | `go test ./cmd/drill` |
| M6.5 | App bindings | `App` methods against a `t.TempDir()` (no Wails runtime needed for CRUD) | `go test .` |

Windows caveat: engine tests use `sh` and are skipped on Windows via
`requirePOSIX(t)`. One `cmd`-based case is still missing.

Status: `go test ./...` is green across `book`, `engine`, `store`, `cmd/drill`
and the root `app` package. The first run caught four real bugs (flag parsing
after a positional, description-only steps rejected, dotted ids, the books-folder
heuristic) — see `IMPLEMENTATION.md` §11.

---

## M7 — Examples and README `IN PROGRESS`

| ID | Task | Acceptance |
| --- | --- | --- |
| M7.1 | `examples/release-1.4.0.json` | Runs on Linux/macOS with `drill run --dir examples release-1-4-0` |
| M7.2 | `examples/onboarding.json` | Description-only steps; demonstrates that books can be checklists |
| M7.3 | `README.md` | 60-second quickstart for both front ends, the format, and the CLI table |
| M7.4 | Screenshot of the desktop app | Stored in `docs/` and linked from the README |

---

## M8 — Repo hygiene `IN PROGRESS`

| ID | Task | Acceptance |
| --- | --- | --- |
| M8.1 | Add `.gitignore` | Ignores `build/bin/`, `frontend/wailsjs/`, `.cache/`, `*.local.json` |
| M8.2 | Remove `.cache/` (scratch Wails template + Go caches) | `git status` clean of caches |
| M8.3 | Verify `gofmt -l` is empty | No output |
| M8.4 | Decide on a licence | `LICENSE` present, referenced from the README |

> Local builds currently use `GOPATH=$PWD/.cache/gopath` because the sandbox
> cannot write to the default `~/go`. That is an environment workaround, not a
> project dependency — do not commit it.

---

## M9 — Runtime verification `IN PROGRESS`

| ID | Task | Acceptance |
| --- | --- | --- |
| M9.1 | Launch the desktop app | Window opens on the books folder, no console errors |
| M9.2 | End-to-end run | Add a `echo hello` step, run it, see stdout and `passed` |
| M9.3 | Failure path | Run `exit 3`; UI shows `failed · exit 3`, run stops, next step `skipped` |
| M9.4 | Stop button | Long `sleep` step can be stopped; process is reaped |
| M9.5 | Save fidelity | Create a book in the UI, confirm `git diff` shows only the intended JSON |
| M9.6 | CLI/UI parity | The same book produces the same `--json` result from both front ends |

---

## M10 — Packaging and CI `TODO`

| ID | Task | Notes |
| --- | --- | --- |
| M10.1 | CLI release builds for linux/amd64, arm64, darwin/universal, windows/amd64 | `go build ./cmd/drill` cross-compiles with `GOOS/GOARCH` |
| M10.2 | Desktop builds per OS | Needs one runner per platform; Wails does not cross-compile |
| M10.3 | Installers | `.deb`/AppImage, `.dmg`, MSI; Wails supports NSIS for Windows |
| M10.4 | CI workflow | `go vet`, `go test`, `wails build` on the three OSes |
| M10.5 | Code signing / notarisation | Required before wide distribution on macOS/Windows |

---

## M11 — Rename to the final product name `DONE`

Waiting on the naming decision (see chat). When it lands:

| ID | Task |
| --- | --- |
| M11.1 | `go.mod` module path + all internal imports |
| M11.2 | `wails.json` `name`/`outputfilename`, window title, `main.go` title |
| M11.3 | `cmd/steps` → `cmd/<name>`; CLI `usage` text and error prefix |
| M11.4 | Config dir `<UserConfigDir>/<name>` (plus migration from `steps`) |
| M11.5 | Icons regenerate unchanged, but `build/` metadata names update |
| M11.6 | Docs, README, examples |

**Check:** `go build ./... && go test ./... && wails build`

---

## M13 — Step checkmarks `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M13.1 | Decide where ticks live (own state file, not the book) | DONE |
| M13.2 | `internal/progress` store: set/unset/reset, atomic write, corrupt-file tolerance | DONE |
| M13.3 | Bindings `CheckedSteps` / `SetStepChecked` / `ResetProgress` | DONE |
| M13.4 | Per-step checkbox with checked styling | DONE |
| M13.5 | Book-level `done/total` counter + Reset button with confirm | DONE |
| M13.6 | Deleted steps stop counting; deleting a book clears its progress | DONE |
| M13.7 | Tick a few boxes in the app and confirm they survive a restart | TODO |

**Check:** `go test ./internal/progress ./...` and `./build/bin/Drill`

---

## M14 — App icon `DONE`

| ID | Task | Status |
| --- | --- | --- |
| M14.1 | Draw the icon in Go instead of shipping a binary blob | DONE |
| M14.2 | Production 1024² PNG + multi-size Windows `.ico` | DONE |
| M14.3 | Wire the icon into the Linux window (`linux.Options.Icon`) | DONE |
| M14.4 | Three design candidates under `docs/icon/` for review | DONE |
| M14.5 | Pick candidate B (checklist) and make it production | DONE |
| M14.6 | Ship a `.desktop` entry + hicolor PNG so docks/app grids show the icon | TODO |

**Check:** `go run ./tools/icon && wails build`

---

## M12 — Backlog (post-v0.1) `TODO`

Ordered by value, not commitment:

| ID | Idea | Why later |
| --- | --- | --- |
| M12.1 | `drill serve` (HTTP + NDJSON, the pre-desktop transport) | Useful for remote/browser use; adds a listener to secure |
| M12.2 | Variables and secrets with prompt + redaction | Needed for real deployments; changes the format (v2) |
| M12.3 | `drill doctor` (shells, cwd, permissions) | Cheap support win once there are users |
| M12.4 | Optional run report written next to the book | Someone will ask; keep it opt-in to protect git cleanliness |
| M12.5 | Per-step env vars and stdin | Frequently requested; easy in the engine |
| M12.6 | Import another book as a step | Composition; keep the format linear for now |
| M12.7 | Conditional steps (`when:`) | Turns Books into a workflow language — resist until demanded |

---

## Open questions

1. ~~**Name.**~~ Decided: **Drill**. M11 is complete; the config dir is
   `<UserConfigDir>/drill` and the env var is `DRILL_DIR`.
2. **Should `run` write a report by default?** Current answer: no — the result is
   returned, and CI can `tee` it.
3. **Portable-by-default books?** Right now the default shell is `sh` on POSIX
   and `pwsh`/`cmd` on Windows, so a book written on macOS may not run on
   Windows unless it sets `shell`. A future `drill doctor` should flag this.
4. **Do step ids need to be immutable across renames?** They are today. That is
   good for CI references; it does mean renaming a step changes its id only if
   the user edits the file.

---

## Verified on this machine (Linux, Go 1.26, Wails 2.12)

| Check | Result |
| --- | --- |
| `go build ./... && go vet ./...` | pass |
| `go test ./...` | pass (book, engine, store, cmd/drill, app) |
| `go run ./tools/icon` | regenerates `build/appicon.png` + `build/windows/icon.ico` |
| `wails build` | `build/bin/Drill`, 8.7 MB, 18 s cold |
| `drill validate --dir examples` | 2 books valid |
| `drill run release-1-4-0 --dir examples --dry-run` | 5 steps resolved, nothing executed |
| `drill run demo` (temp book) | stdout/stderr captured, exit 7 propagated, fail-fast + `continueOnError` observed |
| Launch `build/bin/Drill` with `DRILL_DIR=examples` | window titled `Drill — Onboarding a new engineer` at 1200x820 |
| Checkmark bindings + UI render path | `go test .` covers set/unset/reset/filtering; the window still renders after the change |

## Suggested next three moves

1. **M6.2 + M6.1** (engine and book tests) — the engine is the part where a bug
   silently corrupts a runbook or hangs a release.
2. **M9.3–M9.6** (manual click-through) — launch verified, but running a step,
   the Stop button, and save fidelity from the UI still need a human or a UI
   automation harness.
3. **M8 + M7** (hygiene, examples, README) — makes the repo something another
   person can clone and use.
