// Guided Business Tasks — the run surface.
//
// Pick a task, fill in a short form, get an answer. The point is that
// somebody who knows the business question but not the application never has
// to read a green screen.
//
// The shape of this UI follows one rule: **the terminal stays visible while a
// task runs.** Choosing and filling in happen in a modal, but the moment Run
// is pressed the modal closes and progress moves to a compact card in the
// corner. Operators need to see what the host is actually doing — it is how
// they come to trust the automation, and when a task stops early the screen it
// stopped on is the whole diagnosis.
(function () {
  "use strict";

  var POLL_MS = 600;

  var modal = null;
  var listEl = null;
  var formEl = null;
  var card = null;
  var pollTimer = 0;
  var tasks = [];
  var current = null; // the task being filled in or run
  var lastParams = {}; // so "Run again" does not retype everything

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type, options);
    }
  }

  function api(path, payload) {
    var opts = { credentials: "same-origin" };
    if (payload !== undefined) {
      opts.method = "POST";
      opts.headers = { "Content-Type": "application/json" };
      opts.body = JSON.stringify(payload);
    }
    return fetch(path, opts).then(function (response) {
      return response
        .json()
        .catch(function () { return {}; })
        .then(function (body) {
          if (!response.ok) {
            throw new Error(body.error || "request failed");
          }
          return body;
        });
    });
  }

  /* ---------------------------------------------------------------- */
  /* Chooser                                                          */
  /* ---------------------------------------------------------------- */

  function renderList() {
    listEl.innerHTML = "";
    if (!tasks.length) {
      // An empty catalogue is the normal first-run state, so it explains
      // itself rather than looking broken.
      var empty = document.createElement("p");
      empty.className = "subtle task-empty";
      empty.textContent =
        "No tasks yet. A task is a recorded screen flow with named inputs and " +
        "a named answer — record a flow, then save it as a task.";
      listEl.appendChild(empty);
      return;
    }
    tasks.forEach(function (task) {
      var row = document.createElement("button");
      row.type = "button";
      row.className = "task-row";
      row.setAttribute("data-task-name", task.name);

      var name = document.createElement("span");
      name.className = "task-row-name";
      name.textContent = task.name;
      row.appendChild(name);

      if (task.description) {
        var desc = document.createElement("span");
        desc.className = "task-row-desc";
        desc.textContent = task.description;
        row.appendChild(desc);
      }

      var meta = document.createElement("span");
      meta.className = "task-row-meta";
      meta.textContent =
        (task.parameters || []).length + " input(s) · " + (task.outputs || []).length + " result(s)";
      row.appendChild(meta);

      row.addEventListener("click", function () { showForm(task); });
      listEl.appendChild(row);
    });
  }

  /* ---------------------------------------------------------------- */
  /* Parameter form                                                   */
  /* ---------------------------------------------------------------- */

  function showForm(task) {
    current = task;
    modal.setAttribute("data-task-view", "form");
    formEl.innerHTML = "";

    var heading = document.createElement("h4");
    heading.className = "task-form-title";
    heading.textContent = task.name;
    formEl.appendChild(heading);

    if (task.description) {
      var desc = document.createElement("p");
      desc.className = "subtle task-form-desc";
      desc.textContent = task.description;
      formEl.appendChild(desc);
    }

    var params = task.parameters || [];
    if (!params.length) {
      var none = document.createElement("p");
      none.className = "subtle";
      none.textContent = "This task needs no input.";
      formEl.appendChild(none);
    }

    params.forEach(function (p) {
      var label = document.createElement("label");
      label.className = "task-field";

      var text = document.createElement("span");
      text.className = "task-field-label";
      text.textContent = p.label || p.name;
      if (p.required) {
        var req = document.createElement("span");
        req.className = "task-field-required";
        req.textContent = "required";
        text.appendChild(req);
      }
      label.appendChild(text);

      var input = document.createElement("input");
      // A sensitive parameter is a password: masked here, and the server
      // keeps it out of every result and log line.
      input.type = p.sensitive ? "password" : "text";
      input.name = p.name;
      input.autocomplete = p.sensitive ? "new-password" : "off";
      if (p.maxLength) {
        input.maxLength = p.maxLength;
      }
      if (p.example) {
        input.placeholder = p.example;
      }
      if (lastParams[p.name] && !p.sensitive) {
        input.value = lastParams[p.name];
      } else if (p.default) {
        input.value = p.default;
      }
      if (p.required) {
        input.required = true;
      }
      label.appendChild(input);

      if (p.description) {
        var hint = document.createElement("span");
        hint.className = "task-field-hint subtle";
        hint.textContent = p.description;
        label.appendChild(hint);
      }
      formEl.appendChild(label);
    });

    var actions = document.createElement("div");
    actions.className = "task-form-actions";

    var status = document.createElement("span");
    status.className = "task-form-status";
    status.setAttribute("data-task-form-status", "");
    status.setAttribute("aria-live", "polite");
    actions.appendChild(status);

    var back = document.createElement("button");
    back.type = "button";
    back.textContent = "Back";
    back.addEventListener("click", showChooser);
    actions.appendChild(back);

    var run = document.createElement("button");
    run.type = "submit";
    run.className = "task-run-btn";
    run.textContent = "Run";
    actions.appendChild(run);

    formEl.appendChild(actions);

    var first = formEl.querySelector("input");
    if (first) {
      first.focus();
    }
  }

  function showChooser() {
    current = null;
    modal.setAttribute("data-task-view", "list");
  }

  function collectParams() {
    var out = {};
    var inputs = formEl.querySelectorAll("input");
    for (var i = 0; i < inputs.length; i++) {
      out[inputs[i].name] = inputs[i].value;
    }
    return out;
  }

  function formStatus(message, isError) {
    var el = formEl.querySelector("[data-task-form-status]");
    if (!el) {
      return;
    }
    el.textContent = message || "";
    el.classList.toggle("is-error", !!isError);
  }

  function submit(event) {
    event.preventDefault();
    if (!current) {
      return;
    }
    var params = collectParams();
    formStatus("Starting…");
    api("/tasks/run", { name: current.name, parameters: params }).then(
      function () {
        // Remember non-sensitive values so "Run again" is one click.
        lastParams = {};
        (current.parameters || []).forEach(function (p) {
          if (!p.sensitive) {
            lastParams[p.name] = params[p.name];
          }
        });
        close();
        showProgress(current);
        poll();
      },
      function (err) {
        // A rejected parameter belongs beside the form, not in a toast that
        // disappears while the operator is still reading it.
        formStatus((err && err.message) || "The task could not be started.", true);
      }
    );
  }

  /* ---------------------------------------------------------------- */
  /* Progress and result — a card, so the terminal stays visible      */
  /* ---------------------------------------------------------------- */

  // The card is fixed to the viewport but must never cover the OIA — that bar
  // carries X SYSTEM and the operator-error indicator, and hiding it would
  // undo the reason it exists. Its position moves with the terminal model, the
  // window size and focus mode, so a fixed offset would be wrong most of the
  // time. Measure it instead and publish the clearance as a custom property.
  function positionCard() {
    var oia = document.querySelector(".screen-oia");
    var bottom = 96;
    if (oia) {
      var rect = oia.getBoundingClientRect();
      if (rect.height > 0) {
        bottom = Math.max(16, Math.round(window.innerHeight - rect.top) + 12);
      }
    }
    document.documentElement.style.setProperty("--task-card-bottom", bottom + "px");
  }

  function ensureCard() {
    if (card) {
      positionCard();
      return card;
    }
    card = document.createElement("div");
    card.className = "task-card";
    card.setAttribute("data-task-card", "");
    card.setAttribute("role", "status");
    card.setAttribute("aria-live", "polite");
    document.body.appendChild(card);
    positionCard();
    return card;
  }

  function hideCard() {
    if (card) {
      card.remove();
      card = null;
    }
  }

  function showProgress(task) {
    var el = ensureCard();
    el.setAttribute("data-task-state", "running");
    el.innerHTML = "";

    var head = document.createElement("div");
    head.className = "task-card-head";
    var title = document.createElement("strong");
    title.textContent = task.name;
    head.appendChild(title);
    el.appendChild(head);

    var step = document.createElement("p");
    step.className = "task-card-step";
    step.setAttribute("data-task-step", "");
    step.textContent = "Working…";
    el.appendChild(step);

    var actions = document.createElement("div");
    actions.className = "task-card-actions";
    var cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "danger";
    cancel.textContent = "Cancel";
    cancel.addEventListener("click", function () {
      cancel.disabled = true;
      cancel.textContent = "Cancelling…";
      api("/tasks/cancel", {}).catch(function () { /* the poll reports the outcome */ });
    });
    actions.appendChild(cancel);
    el.appendChild(actions);
  }

  function poll() {
    if (pollTimer) {
      window.clearTimeout(pollTimer);
    }
    api("/tasks/status").then(
      function (status) {
        if (status.running) {
          var step = card && card.querySelector("[data-task-step]");
          if (step) {
            step.textContent = "Running " + status.steps + " step(s) against the host…";
          }
          pollTimer = window.setTimeout(poll, POLL_MS);
          return;
        }
        if (status.error) {
          showFailure(status.task, { reason: status.error });
          return;
        }
        if (status.result) {
          if (status.result.completed) {
            showResult(status.result);
          } else {
            showFailure(status.task, status.result.failure || {}, status.result);
          }
          return;
        }
        hideCard();
      },
      function () {
        // Losing the status endpoint mid-run is itself worth saying; going
        // quiet would leave the card claiming to be working forever.
        showFailure(current ? current.name : "Task", {
          reason: "Lost contact with the server while the task was running."
        });
      }
    );
  }

  function outputRows(result) {
    var wrap = document.createElement("dl");
    wrap.className = "task-result-values";
    (result.outputs || []).forEach(function (o) {
      var dt = document.createElement("dt");
      dt.textContent = o.label || o.name;
      var dd = document.createElement("dd");
      if (o.found) {
        dd.textContent = o.value;
      } else {
        // Never render a blank where a value should be: an empty row reads
        // as "the answer is nothing", which is not what happened.
        dd.textContent = "not found";
        dd.classList.add("is-missing");
      }
      wrap.appendChild(dt);
      wrap.appendChild(dd);
    });
    return wrap;
  }

  function resultText(result) {
    return (result.outputs || [])
      .map(function (o) { return (o.label || o.name) + ": " + (o.found ? o.value : "not found"); })
      .join("\n");
  }

  function resultCsv(result) {
    var esc = function (v) { return '"' + String(v).replace(/"/g, '""') + '"'; };
    var lines = ["Field,Value"];
    (result.outputs || []).forEach(function (o) {
      lines.push(esc(o.label || o.name) + "," + esc(o.found ? o.value : ""));
    });
    return lines.join("\r\n");
  }

  function download(name, text, mime) {
    var blob = new Blob([text], { type: mime });
    var url = URL.createObjectURL(blob);
    var link = document.createElement("a");
    link.href = url;
    link.download = name;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.setTimeout(function () { URL.revokeObjectURL(url); }, 10000);
  }

  function addCommonActions(el, taskName, result) {
    var actions = document.createElement("div");
    actions.className = "task-card-actions";

    if (result) {
      var copy = document.createElement("button");
      copy.type = "button";
      copy.textContent = "Copy";
      copy.addEventListener("click", function () {
        var text = resultText(result);
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(
            function () { notify("Result copied.", "success"); },
            function () { notify("Could not copy the result.", "error"); }
          );
        } else {
          notify("Could not copy the result.", "error");
        }
      });
      actions.appendChild(copy);

      var csv = document.createElement("button");
      csv.type = "button";
      csv.textContent = "CSV";
      csv.addEventListener("click", function () {
        download(
          taskName.replace(/[^A-Za-z0-9]+/g, "-").toLowerCase() + ".csv",
          resultCsv(result),
          "text/csv"
        );
      });
      actions.appendChild(csv);
    }

    var again = document.createElement("button");
    again.type = "button";
    again.textContent = "Run again";
    again.addEventListener("click", function () {
      hideCard();
      var task = findTask(taskName);
      open().then(function () {
        if (task) {
          showForm(task);
        }
      });
    });
    actions.appendChild(again);

    var dismiss = document.createElement("button");
    dismiss.type = "button";
    dismiss.textContent = "Close";
    dismiss.addEventListener("click", hideCard);
    actions.appendChild(dismiss);

    el.appendChild(actions);
  }

  function showResult(result) {
    var el = ensureCard();
    el.setAttribute("data-task-state", "done");
    el.innerHTML = "";

    var head = document.createElement("div");
    head.className = "task-card-head";
    var title = document.createElement("strong");
    title.textContent = result.task;
    head.appendChild(title);
    var took = document.createElement("span");
    took.className = "task-card-took subtle";
    took.textContent = formatDuration(result.durationMs);
    head.appendChild(took);
    el.appendChild(head);

    el.appendChild(outputRows(result));
    addCommonActions(el, result.task, result);
  }

  function showFailure(taskName, failure, result) {
    var el = ensureCard();
    el.setAttribute("data-task-state", "failed");
    el.innerHTML = "";

    var head = document.createElement("div");
    head.className = "task-card-head";
    var title = document.createElement("strong");
    title.textContent = taskName || "Task";
    head.appendChild(title);
    if (result && result.durationMs) {
      var took = document.createElement("span");
      took.className = "task-card-took subtle";
      took.textContent = formatDuration(result.durationMs);
      head.appendChild(took);
    }
    el.appendChild(head);

    var reason = document.createElement("p");
    reason.className = "task-card-reason";
    reason.textContent =
      (failure.step ? "Stopped at step " + failure.step + ". " : "") +
      (failure.reason || "The task did not finish.");
    el.appendChild(reason);

    // Expected-versus-found is the difference between "it broke" and knowing
    // which screen the application actually showed.
    if (failure.expected || failure.actual) {
      var diff = document.createElement("dl");
      diff.className = "task-card-diff";
      var addRow = function (label, value) {
        var dt = document.createElement("dt");
        dt.textContent = label;
        var dd = document.createElement("dd");
        dd.textContent = value;
        diff.appendChild(dt);
        diff.appendChild(dd);
      };
      if (failure.expected) { addRow("Expected", failure.expected); }
      if (failure.actual !== undefined) { addRow("Found", failure.actual || "(blank)"); }
      el.appendChild(diff);
    }

    if (failure.screen) {
      var details = document.createElement("details");
      details.className = "task-card-screen";
      var summary = document.createElement("summary");
      summary.textContent = "The screen it stopped on";
      details.appendChild(summary);
      var pre = document.createElement("pre");
      pre.textContent = failure.screen;
      details.appendChild(pre);
      el.appendChild(details);
    }

    var note = document.createElement("p");
    note.className = "subtle task-card-note";
    note.textContent = "The terminal is left where the task stopped, so you can carry on by hand.";
    el.appendChild(note);

    // Partial output is still worth offering — often most of the answer was
    // read and only one field was missing.
    addCommonActions(el, taskName, result && (result.outputs || []).length ? result : null);
  }

  function formatDuration(ms) {
    if (!ms && ms !== 0) {
      return "";
    }
    if (ms < 1000) {
      return ms + " ms";
    }
    return (ms / 1000).toFixed(1) + " s";
  }

  function findTask(name) {
    for (var i = 0; i < tasks.length; i++) {
      if (tasks[i].name === name) {
        return tasks[i];
      }
    }
    return null;
  }

  /* ---------------------------------------------------------------- */
  /* Modal shell                                                      */
  /* ---------------------------------------------------------------- */

  function build() {
    modal = document.createElement("div");
    modal.className = "tasks-modal";
    modal.hidden = true;
    modal.setAttribute("data-tasks-modal", "");
    modal.setAttribute("data-task-view", "list");
    modal.innerHTML = [
      '<div class="tasks-modal-backdrop" data-tasks-close></div>',
      '<div class="tasks-modal-content" role="dialog" aria-modal="true" aria-labelledby="tasks-title">',
      '  <div class="tasks-modal-header">',
      '    <h3 id="tasks-title">Tasks</h3>',
      '    <button type="button" data-tasks-close>Close</button>',
      "  </div>",
      '  <div class="tasks-list" data-tasks-list></div>',
      '  <form class="tasks-form" data-tasks-form></form>',
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    listEl = modal.querySelector("[data-tasks-list]");
    formEl = modal.querySelector("[data-tasks-form]");
    formEl.addEventListener("submit", submit);

    var closers = modal.querySelectorAll("[data-tasks-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }
    document.addEventListener(
      "keydown",
      function (event) {
        if (isOpen() && event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          close();
        }
      },
      true
    );
  }

  function open() {
    if (!modal) {
      return Promise.resolve();
    }
    modal.hidden = false;
    showChooser();
    listEl.innerHTML = '<p class="subtle">Loading…</p>';
    return api("/tasks").then(
      function (body) {
        tasks = body.tasks || [];
        renderList();
      },
      function (err) {
        listEl.innerHTML = "";
        var p = document.createElement("p");
        p.className = "task-form-status is-error";
        p.textContent = (err && err.message) || "Could not load the task catalogue.";
        listEl.appendChild(p);
      }
    );
  }

  function close() {
    if (modal) {
      modal.hidden = true;
    }
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }
    build();
    var trigger = document.querySelector("[data-tasks-open]");
    if (trigger) {
      trigger.addEventListener("click", function () { open(); });
    }
    // Focus mode, a model change and a plain window resize all move the OIA.
    window.addEventListener("resize", positionCard);
    window.addEventListener("h3270:layout-changed", positionCard);
    // A run survives a page reload, because it is server-side. Pick it back up
    // rather than leaving the operator wondering whether it is still going.
    api("/tasks/status").then(function (status) {
      if (status && (status.running || status.result)) {
        showProgress({ name: status.task || "Task" });
        poll();
      }
    }, function () { /* no session yet */ });
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.tasks = { open: open, close: close, isOpen: isOpen };
})();
