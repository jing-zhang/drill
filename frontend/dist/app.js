"use strict";

/* ------------------------------------------------------------------ */
/* backend bridge                                                      */
/* ------------------------------------------------------------------ */

function errText(e) {
  if (e === null || e === undefined) return "Unknown error";
  if (typeof e === "string") return e;
  if (e.message) return e.message;
  try {
    return JSON.stringify(e);
  } catch (_) {
    return String(e);
  }
}

async function call(method, ...args) {
  const app = window.go && window.go.main && window.go.main.App;
  const fn = app && app[method];
  if (!fn) throw new Error("Backend is not ready yet");
  try {
    return await fn(...args);
  } catch (e) {
    throw new Error(errText(e));
  }
}

const native = () => window.runtime || null;

function wireRuntime() {
  const rt = native();
  if (!rt || typeof rt.EventsOn !== "function") {
    setTimeout(wireRuntime, 100);
    return;
  }
  if (wireRuntime.done) return;
  wireRuntime.done = true;
  rt.EventsOn("run:event", handleEvent);
}

/* ------------------------------------------------------------------ */
/* state                                                               */
/* ------------------------------------------------------------------ */

const state = {
  dir: "",
  books: [],
  problems: [],
  book: null,
  view: "none", // none | book | new-book
  bookForm: null,
  stepForm: null,
  running: false,
  checked: new Set(), // step ids ticked off in the current book
};

const blankStep = () => ({
  id: "",
  title: "",
  description: "",
  command: "",
  script: "",
  shell: "",
  cwd: "",
  timeoutSeconds: 0,
  continueOnError: false,
});

/* ------------------------------------------------------------------ */
/* helpers                                                             */
/* ------------------------------------------------------------------ */

const qs = (sel) => document.querySelector(sel);

const esc = (s) =>
  String(s === null || s === undefined ? "" : s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[c]));

const bookPath = () => {
  const b = state.books.find((x) => x.id === (state.book && state.book.id));
  return b ? b.path : "";
};

const stepEl = (id) => {
  for (const el of document.querySelectorAll(".step")) {
    if (el.dataset.step === id) return el;
  }
  return null;
};

function toast(message, kind) {
  const el = qs("#toast");
  el.textContent = message;
  el.className = "toast" + (kind === "error" ? " error" : "");
  el.hidden = false;
  clearTimeout(toast._t);
  toast._t = setTimeout(() => {
    el.hidden = true;
  }, kind === "error" ? 6000 : 2600);
}

/* ------------------------------------------------------------------ */
/* minimal markdown                                                    */
/* ------------------------------------------------------------------ */

function inline(text) {
  let s = esc(text);
  s = s.replace(/`([^`]+)`/g, (_, code) => `<code>${code}</code>`);
  s = s.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_, label, url) => {
    const safe = /^(https?:\/\/|mailto:|\/|\.\.?\/|#)/i.test(url) ? url : "#";
    return `<a href="${safe}" target="_blank" rel="noreferrer">${label}</a>`;
  });
  s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  s = s.replace(/(^|[\s(])\*([^*\n]+)\*/g, "$1<em>$2</em>");
  s = s.replace(/(^|[\s(])_([^_\n]+)_/g, "$1<em>$2</em>");
  s = s.replace(/~~([^~]+)~~/g, "<del>$1</del>");
  return s;
}

function md(src) {
  const lines = String(src || "").replace(/\r\n/g, "\n").split("\n");
  let html = "";
  let inCode = false;
  let list = null;
  let para = [];

  const flushPara = () => {
    if (para.length) {
      html += `<p>${inline(para.join(" "))}</p>`;
      para = [];
    }
  };
  const closeList = () => {
    if (list) {
      html += `</${list}>`;
      list = null;
    }
  };

  for (const raw of lines) {
    if (/^\s*```/.test(raw)) {
      if (inCode) {
        html += "</code></pre>";
        inCode = false;
      } else {
        flushPara();
        closeList();
        html += "<pre><code>";
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      html += esc(raw) + "\n";
      continue;
    }

    const line = raw.trim();
    if (!line) {
      flushPara();
      closeList();
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      flushPara();
      closeList();
      const level = heading[1].length;
      html += `<h${level}>${inline(heading[2])}</h${level}>`;
      continue;
    }

    const bullet = /^[-*+]\s+(.*)$/.exec(line);
    const numbered = /^\d+[.)]\s+(.*)$/.exec(line);
    if (bullet || numbered) {
      flushPara();
      const want = bullet ? "ul" : "ol";
      if (list && list !== want) closeList();
      if (!list) {
        list = want;
        html += `<${want}>`;
      }
      html += `<li>${inline((bullet || numbered)[1])}</li>`;
      continue;
    }

    if (list) closeList();
    para.push(line);
  }

  flushPara();
  closeList();
  if (inCode) html += "</code></pre>";
  return html;
}

/* ------------------------------------------------------------------ */
/* data                                                                */
/* ------------------------------------------------------------------ */

async function loadAll(selectId) {
  const listing = await call("ListBooks");
  state.dir = listing.dir;
  state.books = listing.books || [];
  state.problems = listing.problems || [];

  let id = selectId || (state.book && state.book.id) || null;
  if (!id || !state.books.some((b) => b.id === id)) {
    id = state.books.length ? state.books[0].id : null;
  }
  if (id) {
    state.book = await call("GetBook", id);
    state.checked = new Set(await call("CheckedSteps", id));
    state.view = "book";
  } else {
    state.book = null;
    state.checked = new Set();
    state.view = "none";
  }
  render();
}

/* ------------------------------------------------------------------ */
/* rendering                                                           */
/* ------------------------------------------------------------------ */

function render() {
  renderSidebar();
  renderMain();
  syncTitle();
}

function syncTitle() {
  const rt = native();
  if (!rt || !rt.WindowSetTitle) return;
  rt.WindowSetTitle(state.book ? `Drill — ${state.book.title}` : "Drill");
}

function renderSidebar() {
  const list = qs("#book-list");
  list.innerHTML = state.books.length
    ? state.books
        .map(
          (b) => `
      <button class="book ${state.book && state.book.id === b.id ? "active" : ""}"
              data-act="select-book" data-id="${esc(b.id)}">
        <span class="book-title">${esc(b.title)}</span>
        <span class="book-meta">${b.stepCount} step${b.stepCount === 1 ? "" : "s"}</span>
      </button>`
        )
        .join("")
    : `<p class="muted small">No books in this folder yet.</p>`;

  qs("#problems").innerHTML = state.problems.length
    ? `<div class="problem"><strong>${state.problems.length} file${
        state.problems.length === 1 ? "" : "s"
      } failed to load</strong>${state.problems
        .map(
          (p) =>
            `<div class="mono small">${esc(p.path)}</div><div class="err small">${esc(p.error)}</div>`
        )
        .join("")}</div>`
    : "";

  const dir = qs("#dir");
  dir.textContent = state.dir;
  dir.title = state.dir;
}

function renderMain() {
  const main = qs("#main");

  if (state.view === "new-book") {
    main.innerHTML = `<header class="book-header"><div class="book-heading"><h2>New book</h2>
      <p class="muted">A book is one process: an ordered list of steps. It is saved as a plain JSON file you can commit next to the thing it describes.</p></div></header>
      ${renderBookForm()}`;
    return;
  }

  if (!state.book) {
    main.innerHTML = `<div class="empty">
      <h2>No book selected</h2>
      <p>Create a book, or use <strong>Change folder</strong> to open a folder of runbooks — for example a <code>runbooks/</code> directory in a git repository.</p>
    </div>`;
    return;
  }

  const b = state.book;
  const path = bookPath();
  const done = b.steps.filter((s) => state.checked.has(s.id)).length;

  main.innerHTML = `
    <header class="book-header">
      ${
        state.bookForm
          ? renderBookForm()
          : `<div class="book-heading">
              <h2>${esc(b.title)}</h2>
              ${b.description ? `<div class="md">${md(b.description)}</div>` : ""}
              ${path ? `<div class="path mono">${esc(path)}</div>` : ""}
            </div>
            <div class="toolbar">
              <span class="progress" id="progress-label">${done}/${b.steps.length} done</span>
              <button class="btn" data-act="reset-progress" title="Clear every checkmark and start fresh" ${
                done === 0 || state.running ? "disabled" : ""
              }>Reset</button>
              <button class="btn primary" data-act="run-all" ${state.running ? "disabled" : ""}>Run all</button>
              <button class="btn" data-act="dry-run" ${state.running ? "disabled" : ""}>Dry run</button>
              <button class="btn danger" data-act="stop-run" ${state.running ? "" : "hidden"}>Stop</button>
              <button class="btn" data-act="edit-book" ${state.running ? "disabled" : ""}>Edit</button>
              <button class="btn danger" data-act="delete-book" ${state.running ? "disabled" : ""}>Delete</button>
            </div>`
      }
    </header>
    <section class="steps">
      ${b.steps
        .map((s, i) =>
          state.stepForm && state.stepForm.mode === "edit" && state.stepForm.index === i
            ? renderStepForm()
            : renderStep(s, i)
        )
        .join("")}
      ${
        b.steps.length === 0 && !state.stepForm
          ? `<p class="muted">No steps yet. A step has a title, a markdown description, and optionally a command or script to run.</p>`
          : ""
      }
      ${
        state.stepForm && state.stepForm.mode === "new"
          ? renderStepForm()
          : `<button class="btn dashed block" data-act="add-step" ${state.running ? "disabled" : ""}>+ Add step</button>`
      }
    </section>`;
}

function renderStep(s, i) {
  const runnable = Boolean(s.command || s.script);
  const shell = s.shell || "default";
  const isChecked = state.checked.has(s.id);
  let payload = "";

  if (s.command) {
    payload = `<pre class="cmd"><span class="tag">${esc(shell)}</span><code>${esc(
      s.command
    )}</code></pre>`;
  } else if (s.script) {
    const lines = s.script.split("\n").length;
    payload = `<details class="script">
        <summary><span class="tag">${esc(shell)}</span> inline script · ${lines} line${
      lines === 1 ? "" : "s"
    }</summary>
        <pre><code>${esc(s.script)}</code></pre>
      </details>`;
  }

  return `
  <article class="step${isChecked ? " checked" : ""}" data-step="${esc(s.id)}">
    <div class="step-head">
      <label class="tick" title="${isChecked ? "Done — click to uncheck" : "Mark this step as done"}">
        <input type="checkbox" data-act="toggle-step" data-id="${esc(s.id)}" ${
    isChecked ? "checked" : ""
  }>
        <span class="tick-box"></span>
      </label>
      <span class="num">${i + 1}</span>
      <h3>${esc(s.title)}</h3>
      <div class="step-actions">
        ${
          runnable
            ? `<button class="btn small primary" data-act="run-step" data-id="${esc(
                s.id
              )}" ${state.running ? "disabled" : ""}>Run</button>`
            : ""
        }
        <button class="btn small" data-act="edit-step" data-index="${i}" ${
    state.running ? "disabled" : ""
  }>Edit</button>
        <button class="btn small icon" data-act="move-up" data-index="${i}" ${
    i === 0 || state.running ? "disabled" : ""
  } title="Move up">↑</button>
        <button class="btn small icon" data-act="move-down" data-index="${i}" ${
    i === state.book.steps.length - 1 || state.running ? "disabled" : ""
  } title="Move down">↓</button>
        <button class="btn small danger icon" data-act="delete-step" data-index="${i}" ${
    state.running ? "disabled" : ""
  } title="Delete">✕</button>
      </div>
    </div>
    ${s.description ? `<div class="md">${md(s.description)}</div>` : ""}
    ${payload}
    ${s.continueOnError ? `<p class="hint">Continues to the next step if this one fails.</p>` : ""}
    <div class="result" data-result="${esc(s.id)}" hidden>
      <div class="result-head"></div>
      <pre class="out"></pre>
    </div>
  </article>`;
}

function renderStepForm() {
  const f = state.stepForm;
  const s = f.step;
  const kind = s.script ? "script" : s.command ? "command" : "none";

  return `
  <form class="step-form card" data-form="step">
    <div class="field">
      <label>Title</label>
      <input name="title" value="${esc(s.title)}" required>
    </div>
    <div class="field">
      <label>Description <span class="muted small">(markdown, optional)</span></label>
      <textarea name="description" rows="3" placeholder="What this step does and how to verify it.">${esc(
        s.description
      )}</textarea>
    </div>
    <div class="field">
      <label>Payload</label>
      <div class="segmented">
        ${["none", "command", "script"]
          .map(
            (k) =>
              `<label class="radio"><input type="radio" name="kind" value="${k}" ${
                kind === k ? "checked" : ""
              }> ${k}</label>`
          )
          .join("")}
      </div>
    </div>
    <div class="field" data-kind="command" ${kind !== "command" ? "hidden" : ""}>
      <label>Command</label>
      <textarea name="command" rows="2" class="mono" placeholder="go test ./...">${esc(
        s.command
      )}</textarea>
    </div>
    <div class="field" data-kind="script" ${kind !== "script" ? "hidden" : ""}>
      <label>Script</label>
      <textarea name="script" rows="7" class="mono" placeholder="set -euo pipefail&#10;echo &quot;hello&quot;">${esc(
        s.script
      )}</textarea>
    </div>
    <div class="row3">
      <div class="field">
        <label>Shell</label>
        <select name="shell">
          <option value="">default</option>
          ${["sh", "bash", "pwsh", "powershell", "cmd", "python"]
            .map(
              (x) =>
                `<option value="${x}" ${s.shell === x ? "selected" : ""}>${x}</option>`
            )
            .join("")}
        </select>
      </div>
      <div class="field">
        <label>Working dir</label>
        <input name="cwd" class="mono" value="${esc(s.cwd)}" placeholder="relative to the book">
      </div>
      <div class="field">
        <label>Timeout (s)</label>
        <input name="timeout" type="number" min="0" value="${
          s.timeoutSeconds || ""
        }" placeholder="600">
      </div>
    </div>
    <label class="check">
      <input type="checkbox" name="continueOnError" ${s.continueOnError ? "checked" : ""}>
      Continue to the next step if this fails
    </label>
    <div class="form-actions">
      <button class="btn primary" type="submit">${f.mode === "new" ? "Add step" : "Save step"}</button>
      <button class="btn" type="button" data-act="cancel-step">Cancel</button>
    </div>
  </form>`;
}

function renderBookForm() {
  const f = state.bookForm;
  return `
  <form class="card book-form" data-form="book">
    ${
      f.mode === "new"
        ? `<div class="field">
             <label>ID <span class="muted small">(optional — derived from the title)</span></label>
             <input name="id" class="mono" value="${esc(f.id)}" placeholder="release-1-4-0">
           </div>`
        : ""
    }
    <div class="field">
      <label>Title</label>
      <input name="title" value="${esc(f.title)}" required>
    </div>
    <div class="field">
      <label>Description <span class="muted small">(markdown, optional)</span></label>
      <textarea name="description" rows="3">${esc(f.description)}</textarea>
    </div>
    <div class="form-actions">
      <button class="btn primary" type="submit">${f.mode === "new" ? "Create book" : "Save"}</button>
      <button class="btn" type="button" data-act="cancel-book">Cancel</button>
    </div>
  </form>`;
}

/* ------------------------------------------------------------------ */
/* running                                                             */
/* ------------------------------------------------------------------ */

function setRunningUI(running) {
  document.querySelectorAll("[data-act='run-all'], [data-act='dry-run'], [data-act='run-step'], [data-act='add-step'], [data-act='edit-book'], [data-act='edit-step'], [data-act='delete-book'], [data-act='delete-step']")
    .forEach((b) => (b.disabled = running));
  const stop = qs("[data-act='stop-run']");
  if (stop) stop.hidden = !running;
  updateProgressUI();
}

function resetResults(stepIDs) {
  const ids = stepIDs && stepIDs.length ? stepIDs : state.book.steps.map((s) => s.id);
  for (const id of ids) {
    const el = stepEl(id);
    if (!el) continue;
    el.classList.remove("running", "failed");
    const box = el.querySelector(".result");
    box.hidden = true;
    box.querySelector(".result-head").innerHTML = "";
    box.querySelector(".out").textContent = "";
  }
}

function statusPill(status, ev) {
  switch (status) {
    case "passed":
      return `<span class="pill ok">passed</span>`;
    case "failed":
      return `<span class="pill bad">failed${
        ev.timedOut ? " · timeout" : ev.exitCode !== null && ev.exitCode !== undefined && ev.exitCode !== 0 ? ` · exit ${ev.exitCode}` : ""
      }</span>`;
    case "skipped":
      return `<span class="pill">skipped</span>`;
    case "preview":
      return `<span class="pill warn">dry run</span>`;
    default:
      return `<span class="pill">${esc(status)}</span>`;
  }
}

function handleEvent(ev) {
  if (ev.type === "step-start") {
    const el = stepEl(ev.stepId);
    if (!el) return;
    el.classList.add("running");
    el.classList.remove("failed");
    const box = el.querySelector(".result");
    box.hidden = false;
    box.querySelector(".out").textContent = "";
    box.querySelector(".result-head").innerHTML =
      `<span class="pill run">running</span> <span class="muted">${esc(ev.shell || "")}</span>`;
    return;
  }

  if (ev.type === "output") {
    const el = stepEl(ev.stepId);
    if (!el) return;
    const out = el.querySelector(".out");
    const line = document.createElement("span");
    line.className = ev.stream === "stderr" ? "line stderr" : "line";
    line.textContent = ev.text + "\n";
    out.appendChild(line);
    out.scrollTop = out.scrollHeight;
    return;
  }

  if (ev.type === "step-end") {
    const el = stepEl(ev.stepId);
    if (!el) return;
    el.classList.remove("running");
    if (ev.status === "failed") el.classList.add("failed");
    const box = el.querySelector(".result");
    box.hidden = false;
    const ms = ev.durationMs || 0;
    const took = ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
    box.querySelector(".result-head").innerHTML =
      `${statusPill(ev.status, ev)} <span class="muted">${took}</span>` +
      (ev.error ? ` <span class="err small">${esc(ev.error)}</span>` : "");
    return;
  }

  if (ev.type === "run-end") {
    if (ev.ok) toast("Run finished");
    else toast("Run stopped: a step failed", "error");
    return;
  }

  if (ev.type === "error") toast(ev.error, "error");
}

async function toggleStep(id, checked) {
  if (!state.book) return;
  try {
    const ids = await call("SetStepChecked", state.book.id, id, checked);
    state.checked = new Set(ids);
  } catch (err) {
    toast(err.message, "error");
  }
  applyCheckUI();
}

// applyCheckUI updates the existing DOM in place so ticking a box does not
// re-render the page (and never disturbs a running step's output).
function applyCheckUI() {
  for (const el of document.querySelectorAll(".step")) {
    const on = state.checked.has(el.dataset.step);
    el.classList.toggle("checked", on);
    const box = el.querySelector("input[data-act='toggle-step']");
    if (box) box.checked = on;
    const label = el.querySelector(".tick");
    if (label) label.title = on ? "Done — click to uncheck" : "Mark this step as done";
  }
  updateProgressUI();
}

function updateProgressUI() {
  const total = state.book ? state.book.steps.length : 0;
  const done = state.checked ? state.checked.size : 0;
  const label = qs("#progress-label");
  if (label) label.textContent = `${done}/${total} done`;
  const btn = qs("[data-act='reset-progress']");
  if (btn) btn.disabled = done === 0 || state.running;
}

async function startRun(stepIDs, dryRun) {
  if (!state.book || state.running) return;
  state.running = true;
  setRunningUI(true);
  resetResults(stepIDs);
  try {
    await call("RunBook", state.book.id, stepIDs || [], Boolean(dryRun));
  } catch (e) {
    toast(e.message, "error");
  } finally {
    state.running = false;
    setRunningUI(false);
    document.querySelectorAll(".step.running").forEach((el) => el.classList.remove("running"));
  }
}

/* ------------------------------------------------------------------ */
/* mutations                                                           */
/* ------------------------------------------------------------------ */

function readStepForm(form) {
  const fd = new FormData(form);
  const kind = String(fd.get("kind") || "none");
  return {
    id: state.stepForm.step.id || "",
    title: String(fd.get("title") || "").trim(),
    description: String(fd.get("description") || ""),
    command: kind === "command" ? String(fd.get("command") || "") : "",
    script: kind === "script" ? String(fd.get("script") || "") : "",
    shell: String(fd.get("shell") || ""),
    cwd: String(fd.get("cwd") || ""),
    timeoutSeconds: Number(fd.get("timeout") || 0),
    continueOnError: fd.get("continueOnError") === "on",
  };
}

async function submitStep(form) {
  const step = readStepForm(form);
  if (!step.title) throw new Error("A step needs a title");
  if (!step.command && !step.script && step.description.trim() === "") {
    throw new Error("A step needs a command, a script, or a description");
  }

  const mode = state.stepForm.mode;
  if (mode === "new") {
    await call("AddStep", state.book.id, step);
  } else {
    await call("UpdateStep", state.book.id, state.stepForm.step.id, step);
  }
  state.stepForm = null;
  await loadAll(state.book.id);
  toast(mode === "new" ? "Step added" : "Step saved");
}

async function submitBook(form) {
  const fd = new FormData(form);
  const title = String(fd.get("title") || "").trim();
  if (!title) throw new Error("A book needs a title");
  const description = String(fd.get("description") || "");

  if (state.bookForm.mode === "new") {
    const created = await call("CreateBook", {
      version: 1,
      id: String(fd.get("id") || "").trim(),
      title,
      description,
      steps: [],
    });
    state.bookForm = null;
    state.view = "book";
    await loadAll(created.id);
    toast("Book created");
    return;
  }

  const book = { ...state.book, title, description };
  state.book = await call("UpdateBook", book);
  state.bookForm = null;
  await loadAll(state.book.id);
  toast("Saved");
}

/* ------------------------------------------------------------------ */
/* events                                                              */
/* ------------------------------------------------------------------ */

document.addEventListener("click", async (e) => {
  const target = e.target.closest("[data-act]");
  if (!target) return;

  const act = target.dataset.act;
  try {
    switch (act) {
      case "new-book":
        state.bookForm = { mode: "new", id: "", title: "", description: "" };
        state.view = "new-book";
        render();
        break;

      case "cancel-book":
        state.bookForm = null;
        state.view = state.book ? "book" : "none";
        render();
        break;

      case "select-book":
        if (state.bookForm) state.bookForm = null;
        state.stepForm = null;
        await loadAll(target.dataset.id);
        break;

      case "refresh":
        await loadAll();
        toast("Reloaded from disk");
        break;

      case "choose-folder": {
        const dir = await call("ChooseFolder");
        if (dir) {
          state.book = null;
          state.bookForm = null;
          state.stepForm = null;
          await loadAll();
          toast("Folder: " + dir);
        }
        break;
      }

      case "edit-book":
        state.bookForm = {
          mode: "edit",
          title: state.book.title,
          description: state.book.description || "",
        };
        render();
        break;

      case "delete-book": {
        if (!confirm(`Delete the book "${state.book.title}"? The file is removed from disk.`)) break;
        await call("DeleteBook", state.book.id);
        state.book = null;
        await loadAll();
        toast("Book deleted");
        break;
      }

      case "add-step":
        state.stepForm = { mode: "new", index: null, step: blankStep() };
        render();
        break;

      case "edit-step": {
        const i = Number(target.dataset.index);
        state.stepForm = { mode: "edit", index: i, step: { ...state.book.steps[i] } };
        render();
        break;
      }

      case "cancel-step":
        state.stepForm = null;
        render();
        break;

      case "delete-step": {
        const i = Number(target.dataset.index);
        const step = state.book.steps[i];
        if (!confirm(`Delete the step "${step.title}"?`)) break;
        await call("DeleteStep", state.book.id, step.id);
        await loadAll(state.book.id);
        toast("Step deleted");
        break;
      }

      case "move-up":
      case "move-down": {
        const i = Number(target.dataset.index);
        const to = act === "move-up" ? i - 1 : i + 1;
        if (to < 0 || to >= state.book.steps.length) break;
        await call("MoveStep", state.book.id, state.book.steps[i].id, to);
        await loadAll(state.book.id);
        break;
      }

      case "reset-progress": {
        const ticked = state.checked.size;
        if (ticked === 0) break;
        const ok = confirm(
          `Clear ${ticked} checkmark${ticked === 1 ? "" : "s"} and start "${state.book.title}" fresh?`
        );
        if (!ok) break;
        await call("ResetProgress", state.book.id);
        state.checked = new Set();
        applyCheckUI();
        toast("Checkmarks cleared");
        break;
      }

      case "run-all":
        await startRun(null, false);
        break;

      case "dry-run":
        await startRun(null, true);
        break;

      case "run-step":
        await startRun([target.dataset.id], false);
        break;

      case "stop-run":
        await call("StopRun");
        break;
    }
  } catch (err) {
    toast(err.message, "error");
  }
});

document.addEventListener("submit", async (e) => {
  const form = e.target.closest("form[data-form]");
  if (!form) return;
  e.preventDefault();
  try {
    if (form.dataset.form === "step") await submitStep(form);
    else if (form.dataset.form === "book") await submitBook(form);
  } catch (err) {
    toast(err.message, "error");
  }
});

document.addEventListener("change", (e) => {
  if (e.target.matches("[data-act='toggle-step']")) {
    toggleStep(e.target.dataset.id, e.target.checked);
    return;
  }
  if (e.target.name !== "kind") return;
  const form = e.target.closest("form");
  if (!form) return;
  form.querySelectorAll("[data-kind]").forEach((el) => {
    el.hidden = el.dataset.kind !== e.target.value;
  });
});

document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (state.stepForm) {
    state.stepForm = null;
    render();
  } else if (state.bookForm) {
    state.bookForm = null;
    state.view = state.book ? "book" : "none";
    render();
  }
});

/* ------------------------------------------------------------------ */
/* boot                                                                */
/* ------------------------------------------------------------------ */

wireRuntime();
loadAll().catch((err) => toast(err.message, "error"));
