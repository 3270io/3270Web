// Task authoring wizard — turning a recording into a business task.
//
// The server derives what a recording can tell it (see internal/task/draft.go)
// and refuses to guess the rest. This is where a person supplies the rest:
//
//   1. Which recorded values are inputs the operator is asked for, and which
//      are always the same.
//   2. Which regions of the final screen are the answer.
//
// (2) is the reason this exists at all. A recording cannot reveal which part
// of a screen someone cares about, so it is marked by dragging over the
// screen the flow ended on — the same gesture as block copy, over a static
// copy of that screen rather than the live terminal, so it stays put while
// the work is done.
(function () {
  "use strict";

  var modal = null;
  var draft = null;
  var outputs = []; // { name, label, row, column, length }
  var screenPre = null;
  var dragAnchor = null;
  var dragRect = null;

  function grid() {
    return window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
  }

  function notify(message, type) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type);
    }
  }

  function status(message, isError) {
    var el = modal && modal.querySelector("[data-wizard-status]");
    if (!el) {
      return;
    }
    el.textContent = message || "";
    el.classList.toggle("is-error", !!isError);
  }

  /* ---------------------------------------------------------------- */
  /* Loading a draft                                                  */
  /* ---------------------------------------------------------------- */

  function open() {
    if (!modal) {
      return;
    }
    modal.hidden = false;
    outputs = [];
    draft = null;
    var body = modal.querySelector("[data-wizard-body]");
    body.innerHTML = '<p class="subtle">Reading the recording…</p>';
    status("");

    fetch("/tasks/draft", { credentials: "same-origin" })
      .then(function (r) {
        return r.json().then(function (b) {
          if (!r.ok) {
            throw new Error(b.error || "could not read the recording");
          }
          return b;
        });
      })
      .then(
        function (loaded) {
          draft = loaded;
          render();
        },
        function (err) {
          body.innerHTML = "";
          var p = document.createElement("p");
          p.className = "task-form-status is-error";
          p.textContent = (err && err.message) || "Could not read the recording.";
          body.appendChild(p);
          var hint = document.createElement("p");
          hint.className = "subtle";
          hint.textContent =
            "Record a flow first: start recording, work through the screens, then stop.";
          body.appendChild(hint);
        }
      );
  }

  function close() {
    if (modal) {
      modal.hidden = true;
    }
    draft = null;
    screenPre = null;
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  /* ---------------------------------------------------------------- */
  /* Rendering the draft for confirmation                             */
  /* ---------------------------------------------------------------- */

  function render() {
    var body = modal.querySelector("[data-wizard-body]");
    body.innerHTML = "";

    body.appendChild(nameSection());
    if (draft.notes && draft.notes.length) {
      body.appendChild(notesSection());
    }
    body.appendChild(parametersSection());
    body.appendChild(outputsSection());
    renderOutputList();
  }

  function nameSection() {
    var wrap = document.createElement("div");
    wrap.className = "wizard-section";

    var label = document.createElement("label");
    label.className = "task-field";
    var text = document.createElement("span");
    text.className = "task-field-label";
    text.textContent = "Task name";
    label.appendChild(text);
    var input = document.createElement("input");
    input.type = "text";
    input.setAttribute("data-wizard-name", "");
    input.maxLength = 64;
    input.value = draft.task.name === "New task" ? "" : draft.task.name;
    input.placeholder = "Account balance enquiry";
    label.appendChild(input);
    wrap.appendChild(label);

    var desc = document.createElement("label");
    desc.className = "task-field";
    var dtext = document.createElement("span");
    dtext.className = "task-field-label";
    dtext.textContent = "What it does";
    desc.appendChild(dtext);
    var dinput = document.createElement("input");
    dinput.type = "text";
    dinput.setAttribute("data-wizard-description", "");
    dinput.maxLength = 200;
    dinput.value = draft.task.description || "";
    dinput.placeholder = "Retrieves the current cleared balance for an account.";
    desc.appendChild(dinput);
    wrap.appendChild(desc);
    return wrap;
  }

  // The notes are the server saying what it assumed. Showing them is the
  // difference between a wizard that asks and one that quietly guesses.
  function notesSection() {
    var wrap = document.createElement("details");
    wrap.className = "wizard-notes";
    wrap.open = true;
    var summary = document.createElement("summary");
    summary.textContent = "What was assumed (" + draft.notes.length + ")";
    wrap.appendChild(summary);
    var list = document.createElement("ul");
    draft.notes.forEach(function (note) {
      var li = document.createElement("li");
      li.textContent = note;
      list.appendChild(li);
    });
    wrap.appendChild(list);
    return wrap;
  }

  function parametersSection() {
    var wrap = document.createElement("div");
    wrap.className = "wizard-section";
    var h = document.createElement("h4");
    h.textContent = "Inputs";
    wrap.appendChild(h);

    var params = draft.task.parameters || [];
    if (!params.length) {
      var none = document.createElement("p");
      none.className = "subtle";
      none.textContent = "Nothing was typed during the recording, so this task takes no input.";
      wrap.appendChild(none);
      return wrap;
    }

    var intro = document.createElement("p");
    intro.className = "subtle";
    intro.textContent =
      "Everything typed during the recording is an input by default. Switch any that " +
      "should always be the same to a fixed value.";
    wrap.appendChild(intro);

    params.forEach(function (p, i) {
      var row = document.createElement("div");
      row.className = "wizard-param";
      row.setAttribute("data-wizard-param", p.name);

      var label = document.createElement("input");
      label.type = "text";
      label.className = "wizard-param-label";
      label.value = p.label || p.name;
      label.setAttribute("data-param-label", "");
      label.setAttribute("aria-label", "Label for input " + (i + 1));
      row.appendChild(label);

      var mode = document.createElement("select");
      mode.setAttribute("data-param-mode", "");
      mode.setAttribute("aria-label", "How input " + (i + 1) + " is supplied");
      [
        ["ask", "Ask the operator"],
        ["fixed", "Always this value"]
      ].forEach(function (opt) {
        var o = document.createElement("option");
        o.value = opt[0];
        o.textContent = opt[1];
        mode.appendChild(o);
      });
      row.appendChild(mode);

      var value = document.createElement("input");
      value.type = "text";
      value.className = "wizard-param-value";
      value.value = p.example || "";
      value.setAttribute("data-param-value", "");
      value.setAttribute("aria-label", "Recorded value for input " + (i + 1));
      row.appendChild(value);

      var where = document.createElement("span");
      where.className = "wizard-param-where subtle";
      where.textContent = "row " + locationOf(p.name).row + ", col " + locationOf(p.name).column;
      row.appendChild(where);

      // "Ask" uses the value as an example; "fixed" uses it as the value
      // itself. Saying which keeps the box from being ambiguous.
      var syncHint = function () {
        value.placeholder = mode.value === "fixed" ? "value to type every time" : "example";
      };
      mode.addEventListener("change", syncHint);
      syncHint();

      wrap.appendChild(row);
    });
    return wrap;
  }

  function locationOf(paramName) {
    var steps = draft.task.steps || [];
    for (var i = 0; i < steps.length; i++) {
      var inputs = steps[i].inputs || [];
      for (var j = 0; j < inputs.length; j++) {
        if (inputs[j].parameter === paramName) {
          return inputs[j];
        }
      }
    }
    return { row: 0, column: 0 };
  }

  /* ---------------------------------------------------------------- */
  /* Marking the answer on the final screen                           */
  /* ---------------------------------------------------------------- */

  function outputsSection() {
    var wrap = document.createElement("div");
    wrap.className = "wizard-section";
    var h = document.createElement("h4");
    h.textContent = "The answer";
    wrap.appendChild(h);

    var intro = document.createElement("p");
    intro.className = "subtle";
    intro.textContent =
      "Drag across the screen below to mark each value the task should report back. " +
      "This is the part a recording cannot work out on its own.";
    wrap.appendChild(intro);

    if (!draft.finalScreen) {
      var missing = document.createElement("p");
      missing.className = "task-form-status is-error";
      missing.textContent =
        "The screen the flow ended on was not captured, so there is nothing to mark. " +
        "Re-record the flow from the session you want to build the task from.";
      wrap.appendChild(missing);
      return wrap;
    }

    var holder = document.createElement("div");
    holder.className = "wizard-screen";
    holder.setAttribute("data-wizard-screen", "");
    screenPre = document.createElement("pre");
    screenPre.textContent = draft.finalScreen;
    holder.appendChild(screenPre);
    var marquee = document.createElement("div");
    marquee.className = "wizard-marquee";
    marquee.setAttribute("data-wizard-marquee", "");
    marquee.hidden = true;
    holder.appendChild(marquee);
    wrap.appendChild(holder);
    attachPicker(holder, marquee);

    var list = document.createElement("div");
    list.className = "wizard-outputs";
    list.setAttribute("data-wizard-outputs", "");
    wrap.appendChild(list);
    return wrap;
  }

  function screenSize() {
    var lines = (draft.finalScreen || "").split("\n");
    var cols = 0;
    lines.forEach(function (l) {
      if (l.length > cols) {
        cols = l.length;
      }
    });
    return { rows: lines.length, cols: cols || 80 };
  }

  // The same drag gesture as block copy, over a static screen. Reusing
  // screenGrid.metricsFor and pointToCell keeps one definition of "which cell
  // is under the pointer" rather than a second that drifts from it.
  function attachPicker(holder, marquee) {
    var size = screenSize();

    var metricsNow = function () {
      return grid() ? grid().metricsFor(screenPre, size.rows, size.cols) : null;
    };

    var place = function (rect) {
      var m = metricsNow();
      if (!m) {
        return;
      }
      var holderRect = holder.getBoundingClientRect();
      marquee.hidden = false;
      marquee.style.left = m.rect.left - holderRect.left + rect.left * m.cellW + "px";
      marquee.style.top = m.rect.top - holderRect.top + rect.top * m.cellH + "px";
      marquee.style.width = (rect.right - rect.left + 1) * m.cellW + "px";
      marquee.style.height = (rect.bottom - rect.top + 1) * m.cellH + "px";
    };

    holder.addEventListener("pointerdown", function (event) {
      var m = metricsNow();
      if (!m) {
        return;
      }
      event.preventDefault();
      dragAnchor = grid().pointToCell(m, event.clientX, event.clientY);
      dragRect = rectFrom(dragAnchor, dragAnchor);
      place(dragRect);
      holder.setPointerCapture(event.pointerId);
    });

    holder.addEventListener("pointermove", function (event) {
      if (!dragAnchor) {
        return;
      }
      var m = metricsNow();
      if (!m) {
        return;
      }
      dragRect = rectFrom(dragAnchor, grid().pointToCell(m, event.clientX, event.clientY));
      place(dragRect);
    });

    var finish = function () {
      if (!dragAnchor || !dragRect) {
        return;
      }
      // A region is a span on one line: the runner reads one row, and letting
      // a selection wrap would capture the label of whatever sits below it.
      addOutput(dragRect.top, dragRect.left, dragRect.right - dragRect.left + 1);
      dragAnchor = null;
      dragRect = null;
      marquee.hidden = true;
    };
    holder.addEventListener("pointerup", finish);
    holder.addEventListener("pointercancel", function () {
      dragAnchor = null;
      dragRect = null;
      marquee.hidden = true;
    });
  }

  function rectFrom(a, b) {
    // Deliberately single-row: the anchor's row wins, so a slightly diagonal
    // drag marks the line the operator started on rather than a block.
    return {
      top: a.row,
      bottom: a.row,
      left: Math.min(a.col, b.col),
      right: Math.max(a.col, b.col)
    };
  }

  function addOutput(row0, col0, length) {
    var lines = (draft.finalScreen || "").split("\n");
    var line = lines[row0] || "";

    // Extend the region rightwards across trailing spaces, up to the next
    // non-blank character or the end of the row.
    //
    // This matters more than it looks. Someone marks the value they can see —
    // "ADA" — which is three characters wide because that is what the
    // recording happened to contain. Run the task for "GRACE" and a
    // three-character region reports "GRA": silently truncated, and wrong in
    // the way that is hardest to notice. On a screen a value occupies a slot
    // padded with spaces, and the slot is its real extent. Extending only
    // across spaces means an adjacent label or figure is never swallowed,
    // and the runner trims the padding, so a too-wide region costs nothing
    // while a too-narrow one corrupts the answer.
    var end = col0 + length; // exclusive
    while (end < line.length && line.charAt(end) === " ") {
      end++;
    }
    if (end >= line.length) {
      end = line.length;
    }
    var padded = end - col0;
    if (padded > length) {
      length = padded;
    }

    var text = line.slice(col0, col0 + length);
    // Name it from the label to the left where there is one, so the result
    // card reads like the screen it came from.
    var left = line.slice(0, col0).replace(/[ .:_-]+$/, "");
    var idx = left.lastIndexOf("  ");
    var suggested = (idx >= 0 ? left.slice(idx + 2) : left).trim();
    if (!suggested || suggested.length > 40) {
      suggested = "Value " + (outputs.length + 1);
    }
    outputs.push({
      name: slug(suggested) || "value_" + (outputs.length + 1),
      label: suggested,
      row: row0 + 1,
      column: col0 + 1,
      length: length,
      sample: text
    });
    renderOutputList();
    status("Marked " + JSON.stringify(text.trim()) + " as " + suggested + ".");
  }

  function slug(text) {
    var out = text
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "");
    if (!out) {
      return "";
    }
    if (!/^[a-z]/.test(out)) {
      out = "v_" + out;
    }
    return out.slice(0, 64).replace(/_+$/, "");
  }

  function renderOutputList() {
    var list = modal.querySelector("[data-wizard-outputs]");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    if (!outputs.length) {
      var none = document.createElement("p");
      none.className = "subtle";
      none.textContent = "Nothing marked yet. A task with no answer cannot be saved.";
      list.appendChild(none);
      return;
    }
    outputs.forEach(function (o, i) {
      var row = document.createElement("div");
      row.className = "wizard-output";

      var label = document.createElement("input");
      label.type = "text";
      label.value = o.label;
      label.setAttribute("aria-label", "Label for answer " + (i + 1));
      label.addEventListener("input", function () {
        o.label = label.value;
        o.name = slug(label.value) || "value_" + (i + 1);
      });
      row.appendChild(label);

      var sample = document.createElement("code");
      sample.className = "wizard-output-sample";
      sample.textContent = o.sample.trim() || "(blank)";
      row.appendChild(sample);

      var where = document.createElement("span");
      where.className = "subtle";
      where.textContent = "row " + o.row + ", col " + o.column + ", " + o.length + " chars";
      where.title =
        "The region runs to the next text on the row, so a longer value than the " +
        "recorded one is not cut short. Trailing spaces are trimmed from the result.";
      row.appendChild(where);

      var remove = document.createElement("button");
      remove.type = "button";
      remove.textContent = "Remove";
      remove.addEventListener("click", function () {
        outputs.splice(i, 1);
        renderOutputList();
      });
      row.appendChild(remove);

      list.appendChild(row);
    });
  }

  /* ---------------------------------------------------------------- */
  /* Saving                                                           */
  /* ---------------------------------------------------------------- */

  function collect() {
    var task = JSON.parse(JSON.stringify(draft.task));
    task.name = (modal.querySelector("[data-wizard-name]").value || "").trim();
    task.description = (modal.querySelector("[data-wizard-description]").value || "").trim();

    // Walk the parameter rows. "Fixed" parameters are removed and their
    // inputs rewritten to carry the literal, which is what makes the choice
    // real rather than cosmetic.
    var keep = [];
    var fixed = {};
    var rows = modal.querySelectorAll("[data-wizard-param]");
    for (var i = 0; i < rows.length; i++) {
      var name = rows[i].getAttribute("data-wizard-param");
      var mode = rows[i].querySelector("[data-param-mode]").value;
      var label = rows[i].querySelector("[data-param-label]").value.trim();
      var value = rows[i].querySelector("[data-param-value]").value;
      var original = (task.parameters || []).filter(function (p) { return p.name === name; })[0];
      if (!original) {
        continue;
      }
      if (mode === "fixed") {
        fixed[name] = value;
        continue;
      }
      original.label = label || original.label;
      original.example = value;
      keep.push(original);
    }
    task.parameters = keep;
    (task.steps || []).forEach(function (step) {
      (step.inputs || []).forEach(function (input) {
        if (input.parameter && Object.prototype.hasOwnProperty.call(fixed, input.parameter)) {
          input.value = fixed[input.parameter];
          delete input.parameter;
        }
      });
      // An input with neither a value nor a parameter is not an input.
      step.inputs = (step.inputs || []).filter(function (input) {
        return (input.value && input.value !== "") || input.parameter;
      });
    });

    task.outputs = outputs.map(function (o) {
      return { name: o.name, label: o.label, row: o.row, column: o.column, length: o.length };
    });
    return task;
  }

  function save() {
    if (!draft) {
      return;
    }
    var task = collect();
    if (!task.name) {
      status("Give the task a name.", true);
      return;
    }
    if (!task.outputs.length) {
      status("Mark at least one value on the screen as the answer.", true);
      return;
    }
    status("Saving…");
    fetch("/tasks/save", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(task)
    })
      .then(function (r) {
        return r.json().then(function (b) {
          if (!r.ok) {
            throw new Error(b.error || "the task could not be saved");
          }
          return b;
        });
      })
      .then(
        function () {
          notify('Saved "' + task.name + '" to the task catalogue.', "success");
          close();
        },
        function (err) {
          // The server validates the same task the runner will execute, so
          // its complaint is the one worth showing verbatim.
          status((err && err.message) || "The task could not be saved.", true);
        }
      );
  }

  /* ---------------------------------------------------------------- */
  /* Shell                                                            */
  /* ---------------------------------------------------------------- */

  function build() {
    modal = document.createElement("div");
    modal.className = "wizard-modal";
    modal.hidden = true;
    modal.setAttribute("data-wizard-modal", "");
    modal.innerHTML = [
      '<div class="wizard-modal-backdrop" data-wizard-close></div>',
      '<div class="wizard-modal-content" role="dialog" aria-modal="true" aria-labelledby="wizard-title">',
      '  <div class="wizard-modal-header">',
      '    <h3 id="wizard-title">Save recording as a task</h3>',
      '    <button type="button" data-wizard-close>Close</button>',
      "  </div>",
      '  <div class="wizard-body" data-wizard-body></div>',
      '  <div class="wizard-actions">',
      '    <span class="task-form-status" data-wizard-status aria-live="polite"></span>',
      '    <button type="button" data-wizard-save class="task-run-btn">Save task</button>',
      "  </div>",
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    modal.querySelector("[data-wizard-save]").addEventListener("click", save);
    var closers = modal.querySelectorAll("[data-wizard-close]");
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

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }
    build();
    var trigger = document.querySelector("[data-wizard-open]");
    if (trigger) {
      trigger.addEventListener("click", open);
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.taskWizard = { open: open, close: close, isOpen: isOpen };
})();
