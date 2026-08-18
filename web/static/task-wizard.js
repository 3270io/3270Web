// Task authoring wizard — turning a recording into a business task.
//
// The server derives what a recording can tell it (see internal/task/draft.go)
// and refuses to guess the rest. This is where a person supplies the rest, in
// five stages that follow the shape of the task itself:
//
//   Details  what the task is called and what it does
//   Inputs   which recorded values the operator is asked for, which are
//            always the same, and which are secrets that must not be stored
//   Steps    the guard on each step — the check that says "this is the screen
//            I meant" before anything is typed
//   Answer   which regions of the final screen are the result
//   Review   what the server makes of all of it, before it is saved
//
// Two of those stages exist because a recording cannot supply them. Which
// part of a screen is "the answer" is a business judgement, so it is marked by
// dragging over the screen the flow ended on — the same gesture as block copy,
// over a static copy of that screen so it stays put while the work is done.
// And a guard the draft derived may be the panel title or may be the date in
// the corner; a step whose guard is wrong is worse than one with no guard,
// because it looks safe.
//
// The wizard also edits a task that is already in the catalogue. Without that,
// every correction — a mislabelled input, a region one character too short —
// costs a re-recording of the whole flow, and a catalogue fills up with tasks
// nobody dares touch.
(function () {
  "use strict";

  var STAGES = [
    { key: "details", title: "Details" },
    { key: "inputs", title: "Inputs" },
    { key: "steps", title: "Steps" },
    { key: "answer", title: "The answer" },
    { key: "review", title: "Review" }
  ];

  var modal = null;
  var state = null;
  var pickers = []; // live screen pickers on the current stage, for resize

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

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (text !== undefined && text !== null) {
      node.textContent = text;
    }
    return node;
  }

  function clone(value) {
    return JSON.parse(JSON.stringify(value));
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
  /* Loading a draft                                                  */
  /* ---------------------------------------------------------------- */

  // Escape, focus, the Tab trap, the background scroll lock and the focus
  // restore all belong to pushModal/popModal — see modal-utils.js. This dialog
  // is routinely opened from the tasks list, so a private Escape listener here
  // would close both on one press.
  function open(fromTaskName) {
    if (!modal) {
      return Promise.resolve();
    }
    modal.hidden = false;
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      window.ThreeSeventyWeb.pushModal(modal, close);
    }
    state = null;
    pickers = [];
    var body = modal.querySelector("[data-wizard-body]");
    body.innerHTML = "";
    body.appendChild(el("p", "subtle", fromTaskName ? "Opening the task…" : "Reading the recording…"));
    setTitle(fromTaskName ? "Edit task" : "Save recording as a task");
    renderStepper(0);
    status("");

    var url = "/tasks/draft";
    if (fromTaskName) {
      url += "?from=" + encodeURIComponent(fromTaskName);
    }
    return fetch(url, { credentials: "same-origin" })
      .then(function (r) {
        return r.json().then(function (b) {
          if (!r.ok) {
            throw new Error(b.error || "could not read the recording");
          }
          return b;
        });
      })
      .then(
        function (draft) {
          adopt(draft);
          render();
        },
        function (err) {
          body.innerHTML = "";
          var p = el("p", "task-form-status is-error",
            (err && err.message) || "Could not read the recording.");
          body.appendChild(p);
          if (!fromTaskName) {
            body.appendChild(el("p", "subtle",
              "Record a flow first: start recording, work through the screens, then stop."));
          }
        }
      );
  }

  // adopt turns the server's draft into the editable state this dialog works
  // on. Everything the wizard changes lives on state.task, so saving is a
  // projection of it rather than a re-read of the DOM: a value that never
  // reached state is a value the operator can see and the task does not have.
  function adopt(draft) {
    var task = clone(draft.task || {});
    task.parameters = task.parameters || [];
    task.steps = task.steps || [];
    task.outputs = task.outputs || [];

    var modes = {};
    task.parameters.forEach(function (p) {
      // A parameter the server flagged as a secret starts as one. Turning it
      // back into an ordinary input is a click; a password already written
      // into the catalogue is not retrievable.
      modes[p.name] = p.sensitive ? "secret" : "ask";
    });

    state = {
      origin: draft.origin || "recording",
      originalName: draft.originalName || "",
      notes: draft.notes || [],
      finalScreen: draft.finalScreen || "",
      stepScreens: draft.stepScreens || [],
      task: task,
      modes: modes,
      fixed: {},
      stage: 0,
      preview: null
    };
    if (state.task.name === "New task") {
      state.task.name = "";
    }
    setTitle(state.origin === "task" ? "Edit task" : "Save recording as a task");
  }

  function setTitle(text) {
    var h = modal && modal.querySelector("#wizard-title");
    if (h) {
      h.textContent = text;
    }
  }

  function close() {
    if (modal) {
      modal.hidden = true;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
    }
    state = null;
    pickers = [];
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  /* ---------------------------------------------------------------- */
  /* Stage shell                                                      */
  /* ---------------------------------------------------------------- */

  function renderStepper(active) {
    var bar = modal.querySelector("[data-wizard-stepper]");
    if (!bar) {
      return;
    }
    bar.innerHTML = "";
    STAGES.forEach(function (stage, i) {
      // Free navigation rather than a locked sequence. Someone correcting one
      // label in a saved task should not have to walk past four stages to
      // reach it, and the Review stage is the thing that actually checks the
      // task — the ordering is guidance, not a gate.
      var button = el("button", "wizard-step", String(i + 1) + ". " + stage.title);
      button.type = "button";
      button.setAttribute("data-wizard-step-nav", stage.key);
      button.setAttribute("aria-current", i === active ? "step" : "false");
      button.classList.toggle("is-active", i === active);
      button.classList.toggle("is-done", i < active);
      button.addEventListener("click", function () { goTo(i); });
      bar.appendChild(button);
    });
  }

  function goTo(index) {
    if (!state) {
      return;
    }
    state.stage = Math.max(0, Math.min(STAGES.length - 1, index));
    render();
  }

  function render() {
    if (!state) {
      return;
    }
    // Changing stage rebuilds the stage bar, which destroys the very button
    // that was just clicked — and focus lands back on the document body,
    // outside the dialog, where the next Tab starts from the page behind it.
    var hadFocus = modal.contains(document.activeElement);
    pickers = [];
    var body = modal.querySelector("[data-wizard-body]");
    body.innerHTML = "";
    body.setAttribute("data-wizard-stage", STAGES[state.stage].key);
    renderStepper(state.stage);
    status("");

    switch (STAGES[state.stage].key) {
      case "details":
        renderDetails(body);
        break;
      case "inputs":
        renderInputs(body);
        break;
      case "steps":
        renderSteps(body);
        break;
      case "answer":
        renderAnswer(body);
        break;
      case "review":
        renderReview(body);
        break;
    }
    renderActions();
    body.scrollTop = 0;
    if (hadFocus && !modal.contains(document.activeElement)) {
      var active = modal.querySelector(".wizard-step.is-active");
      if (active) {
        active.focus();
      }
    }
    // Overlays can only be placed once the browser has laid the screen out.
    window.requestAnimationFrame(refreshPickers);
  }

  function renderActions() {
    var actions = modal.querySelector("[data-wizard-nav]");
    actions.innerHTML = "";

    if (state.stage > 0) {
      var back = el("button", null, "Back");
      back.type = "button";
      back.addEventListener("click", function () { goTo(state.stage - 1); });
      actions.appendChild(back);
    }
    if (state.stage < STAGES.length - 1) {
      var next = el("button", "task-run-btn", "Next: " + STAGES[state.stage + 1].title);
      next.type = "button";
      next.addEventListener("click", function () { goTo(state.stage + 1); });
      actions.appendChild(next);
    } else {
      var saveButton = el("button", "task-run-btn", "Save task");
      saveButton.type = "button";
      saveButton.setAttribute("data-wizard-save", "");
      saveButton.addEventListener("click", saveTask);
      actions.appendChild(saveButton);
    }
  }

  /* ---------------------------------------------------------------- */
  /* Stage 1 — details                                                */
  /* ---------------------------------------------------------------- */

  function renderDetails(body) {
    var wrap = el("div", "wizard-section");
    wrap.appendChild(el("h4", null, "What is this task?"));
    wrap.appendChild(el("p", "subtle",
      "The name is what appears on the task menu and what an assistant calls " +
      "to run it. Write it as the question somebody is asking."));

    wrap.appendChild(textField("Task name", state.task.name, 64, "Account balance enquiry",
      function (value) { state.task.name = value; }, "data-wizard-name"));
    wrap.appendChild(textField("What it does", state.task.description || "", 200,
      "Retrieves the current cleared balance for an account.",
      function (value) { state.task.description = value; }, "data-wizard-description"));
    body.appendChild(wrap);

    if (state.notes && state.notes.length) {
      body.appendChild(notesSection());
    }

    var summary = el("div", "wizard-section");
    summary.appendChild(el("h4", null, "What the recording gave us"));
    var list = el("ul", "wizard-facts");
    list.appendChild(el("li", null, (state.task.steps || []).length + " step(s) the task will perform"));
    list.appendChild(el("li", null,
      (state.task.parameters || []).length + " value(s) typed during the flow, which become inputs"));
    list.appendChild(el("li", null,
      guardedStepCount() + " of " + (state.task.steps || []).length +
      " step(s) have a guard — a check that the screen is the one expected"));
    list.appendChild(el("li", null, (state.task.outputs || []).length + " answer region(s) marked"));
    summary.appendChild(list);
    body.appendChild(summary);
  }

  function guardedStepCount() {
    return (state.task.steps || []).filter(function (s) {
      return (s.expect || []).length > 0;
    }).length;
  }

  function textField(labelText, value, maxLength, placeholder, onChange, hook) {
    var label = el("label", "task-field");
    label.appendChild(el("span", "task-field-label", labelText));
    var input = document.createElement("input");
    input.type = "text";
    input.maxLength = maxLength;
    input.value = value || "";
    input.placeholder = placeholder || "";
    if (hook) {
      input.setAttribute(hook, "");
    }
    input.addEventListener("input", function () { onChange(input.value); });
    label.appendChild(input);
    return label;
  }

  // The notes are the server saying what it assumed. Showing them is the
  // difference between a wizard that asks and one that quietly guesses.
  function notesSection() {
    var wrap = document.createElement("details");
    wrap.className = "wizard-notes";
    wrap.open = true;
    var summary = document.createElement("summary");
    summary.textContent = "What was assumed (" + state.notes.length + ")";
    wrap.appendChild(summary);
    var list = document.createElement("ul");
    state.notes.forEach(function (note) {
      list.appendChild(el("li", null, note));
    });
    wrap.appendChild(list);
    return wrap;
  }

  /* ---------------------------------------------------------------- */
  /* Stage 2 — inputs                                                 */
  /* ---------------------------------------------------------------- */

  function renderInputs(body) {
    var wrap = el("div", "wizard-section");
    wrap.appendChild(el("h4", null, "Inputs"));

    var params = state.task.parameters || [];
    if (!params.length) {
      wrap.appendChild(el("p", "subtle",
        "Nothing was typed during the recording, so this task takes no input."));
      body.appendChild(wrap);
      return;
    }

    wrap.appendChild(el("p", "subtle",
      "Everything typed during the recording is an input the operator is asked for. " +
      "Switch any that should always be the same to a fixed value, and mark anything " +
      "that is a secret — a secret is masked on the form and is never written into the " +
      "catalogue."));
    body.appendChild(wrap);

    params.forEach(function (p, i) {
      body.appendChild(parameterCard(p, i));
    });
  }

  function parameterCard(p, index) {
    var card = el("div", "wizard-card");
    card.setAttribute("data-wizard-param", p.name);
    var head = el("div", "wizard-card-head");
    var where = locationOf(p.name);
    head.appendChild(el("strong", null, "Input " + (index + 1)));
    head.appendChild(el("span", "subtle",
      "typed at row " + where.row + ", column " + where.column +
      (where.length ? ", " + where.length + " characters wide" : "")));
    card.appendChild(head);

    var row = el("div", "wizard-param");

    var label = document.createElement("input");
    label.type = "text";
    label.className = "wizard-param-label";
    label.value = p.label || p.name;
    label.setAttribute("aria-label", "Label for input " + (index + 1));
    label.setAttribute("data-param-label", "");
    label.addEventListener("input", function () { p.label = label.value; });
    row.appendChild(label);

    var mode = document.createElement("select");
    mode.setAttribute("aria-label", "How input " + (index + 1) + " is supplied");
    mode.setAttribute("data-param-mode", "");
    [
      ["ask", "Ask the operator"],
      ["fixed", "Always this value"],
      ["secret", "Ask, and never store it"]
    ].forEach(function (opt) {
      var o = document.createElement("option");
      o.value = opt[0];
      o.textContent = opt[1];
      mode.appendChild(o);
    });
    mode.value = state.modes[p.name] || "ask";
    row.appendChild(mode);

    var value = document.createElement("input");
    value.type = "text";
    value.className = "wizard-param-value";
    value.value = state.modes[p.name] === "fixed"
      ? (state.fixed[p.name] !== undefined ? state.fixed[p.name] : p.example || "")
      : p.example || "";
    value.setAttribute("aria-label", "Recorded value for input " + (index + 1));
    value.setAttribute("data-param-value", "");
    row.appendChild(value);

    var required = el("label", "wizard-check");
    var requiredBox = document.createElement("input");
    requiredBox.type = "checkbox";
    requiredBox.checked = p.required !== false;
    requiredBox.addEventListener("change", function () { p.required = requiredBox.checked; });
    required.appendChild(requiredBox);
    required.appendChild(el("span", null, "Required"));
    row.appendChild(required);

    card.appendChild(row);

    var warning = el("p", "wizard-warning");
    warning.hidden = true;
    card.appendChild(warning);

    // "Ask" uses the value as an example; "fixed" uses it as the value
    // itself; a secret has neither, because storing an example of a password
    // is storing the password. Saying which keeps the box from being
    // ambiguous.
    var syncMode = function () {
      var m = mode.value;
      state.modes[p.name] = m;
      value.disabled = m === "secret";
      value.placeholder = m === "fixed" ? "value to type every time" : "example";
      requiredBox.disabled = m === "fixed";
      required.classList.toggle("is-disabled", m === "fixed");
      if (m === "secret") {
        value.value = "";
        p.example = "";
        p.sensitive = true;
        warning.hidden = false;
        warning.textContent =
          "Masked on the form and kept out of every result and log line. The host " +
          "still receives it; nothing else does.";
        warning.classList.remove("is-error");
      } else {
        p.sensitive = false;
        if (m === "fixed") {
          warning.hidden = false;
          warning.classList.toggle("is-error", !value.value);
          warning.textContent = value.value
            ? "The operator is not asked for this. " + JSON.stringify(value.value) +
              " is typed into row " + locationOf(p.name).row + " every run."
            : "A fixed value with nothing in it types nothing, and the field is left as the host painted it.";
        } else {
          warning.hidden = true;
        }
      }
    };
    var syncValue = function () {
      if (mode.value === "fixed") {
        state.fixed[p.name] = value.value;
      } else {
        p.example = value.value;
      }
      syncMode();
    };
    mode.addEventListener("change", syncMode);
    value.addEventListener("input", syncValue);
    syncMode();

    card.appendChild(advancedParameter(p));
    return card;
  }

  // The rest of the parameter contract, behind a disclosure because most
  // tasks need none of it — and the ones that do are the ones where a typo
  // reaches the host as an account number.
  function advancedParameter(p) {
    var details = document.createElement("details");
    details.className = "wizard-advanced";
    var summary = document.createElement("summary");
    summary.textContent = "Validation and hint";
    details.appendChild(summary);

    var grid = el("div", "wizard-advanced-grid");
    grid.appendChild(textField("Hint shown under the field", p.description || "", 200,
      "Eight digits, no spaces", function (v) { p.description = v; }));
    grid.appendChild(textField("Must match (regular expression)", p.pattern || "", 200,
      "\\d{8}", function (v) { p.pattern = v; }));

    var maxLabel = el("label", "task-field");
    maxLabel.appendChild(el("span", "task-field-label", "Longest allowed value"));
    var maxInput = document.createElement("input");
    maxInput.type = "number";
    maxInput.min = "0";
    maxInput.max = "160";
    maxInput.value = p.maxLength || 0;
    maxInput.addEventListener("input", function () {
      var n = parseInt(maxInput.value, 10);
      p.maxLength = isNaN(n) || n < 0 ? 0 : Math.min(160, n);
    });
    maxLabel.appendChild(maxInput);
    grid.appendChild(maxLabel);

    details.appendChild(grid);
    details.appendChild(el("p", "subtle",
      "The pattern is anchored at both ends, so \\d{8} means the whole value is " +
      "eight digits rather than \"contains eight digits\". It is checked on the " +
      "server on every run, not only in the form."));
    return details;
  }

  function locationOf(paramName) {
    var steps = state.task.steps || [];
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
  /* Stage 3 — steps and their guards                                 */
  /* ---------------------------------------------------------------- */

  function renderSteps(body) {
    var wrap = el("div", "wizard-section");
    wrap.appendChild(el("h4", null, "Steps and their guards"));
    wrap.appendChild(el("p", "subtle",
      "A guard is what the step checks before it types anything: text that must be " +
      "at a position on the screen. It is the difference between a task that stops " +
      "and says which screen it found, and one that types an account number into " +
      "whatever field is under the cursor."));
    body.appendChild(wrap);

    (state.task.steps || []).forEach(function (step, i) {
      body.appendChild(stepCard(step, i));
    });
  }

  function stepCard(step, index) {
    var card = el("div", "wizard-card");
    card.classList.toggle("is-unguarded", !(step.expect || []).length);

    var head = el("div", "wizard-card-head");
    head.appendChild(el("strong", null, "Step " + (index + 1)));
    head.appendChild(el("span", "subtle", stepSummary(step)));
    card.appendChild(head);

    card.appendChild(textField("What this step does", step.description || "", 200,
      "Enter the account number", function (v) { step.description = v; }));

    card.appendChild(stepInputs(step));

    var guards = el("div", "wizard-guards");
    guards.setAttribute("data-wizard-guards", "");
    card.appendChild(guards);

    var screen = (state.stepScreens[index] || {}).screen || "";
    var candidates = (state.stepScreens[index] || {}).guardCandidates || [];

    var picker = null;
    var redraw = function () {
      renderGuardList(guards, step, index, candidates, function () {
        redraw();
        if (picker) {
          picker.refresh();
        }
        card.classList.toggle("is-unguarded", !(step.expect || []).length);
      });
    };
    redraw();

    if (screen) {
      var holder = el("div", "wizard-screen-wrap");
      holder.appendChild(el("p", "subtle",
        "Drag across this screen to guard on a different piece of text."));
      picker = makePicker(screen, {
        regions: function () {
          return (step.expect || []).map(function (e, i) {
            return {
              row: e.row,
              column: e.column,
              length: e.text.length,
              label: String(i + 1),
              absent: !!e.absent
            };
          });
        },
        onSelect: function (row0, col0, length) {
          var text = lineOf(screen, row0).slice(col0, col0 + length).replace(/\s+$/, "");
          if (!text.trim()) {
            status("That region is blank, so it cannot identify the screen.", true);
            return;
          }
          step.expect = step.expect || [];
          step.expect.push({ row: row0 + 1, column: col0 + 1, text: text });
          status("Guarding step " + (index + 1) + " on " + JSON.stringify(text) + ".");
          redraw();
          picker.refresh();
          card.classList.toggle("is-unguarded", false);
        }
      });
      holder.appendChild(picker.element);
      card.appendChild(holder);
      pickers.push(picker);
    } else if (!(step.expect || []).length) {
      card.appendChild(el("p", "wizard-warning is-error",
        "No screen was captured for this step, so a guard has to be typed in by hand: " +
        "the row, the column and the text that identifies the screen."));
    }

    return card;
  }

  // What the step types, and where.
  //
  // A value switched to "Always this value" leaves the Inputs stage entirely —
  // it is a literal on a step now — and without this it would be invisible
  // from that moment on. A menu selection typed into the wrong column is then
  // a defect with nowhere to be corrected but a re-recording.
  function stepInputs(step) {
    var wrap = el("div", "wizard-guards wizard-inputs");
    var inputs = step.inputs || [];
    if (!inputs.length) {
      return wrap;
    }
    wrap.appendChild(el("span", "subtle", "Types:"));
    inputs.forEach(function (input, i) {
      var row = el("div", "wizard-guard wizard-input-row");
      if (input.parameter) {
        var named = el("span", "wizard-input-param", input.parameter);
        named.title = "An input from the form. Change what it is called, or fix its value, on the Inputs stage.";
        row.appendChild(named);
      } else {
        var value = document.createElement("input");
        value.type = "text";
        value.value = input.value || "";
        value.setAttribute("aria-label", "Fixed value typed by this step");
        value.addEventListener("input", function () { input.value = value.value; });
        row.appendChild(value);
      }
      row.appendChild(numberField("row", input.row, 1, 100, function (v) { input.row = v; }));
      row.appendChild(numberField("col", input.column, 1, 200, function (v) { input.column = v; }));
      if (!input.parameter) {
        // Only a literal can be removed here. Dropping the last step that
        // fills a parameter would leave that parameter unused, which the
        // server refuses — and rightly: it is a form field that does nothing.
        var remove = el("button", null, "Remove");
        remove.type = "button";
        remove.addEventListener("click", function () {
          inputs.splice(i, 1);
          var replacement = stepInputs(step);
          wrap.parentNode.replaceChild(replacement, wrap);
        });
        row.appendChild(remove);
      }
      wrap.appendChild(row);
    });
    return wrap;
  }

  function stepSummary(step) {
    var parts = [];
    var inputs = (step.inputs || []).length;
    if (inputs) {
      parts.push("fills " + inputs + " field(s)");
    }
    parts.push(step.aidKey ? "presses " + step.aidKey : "presses nothing");
    return parts.join(", ");
  }

  function renderGuardList(container, step, stepIndex, candidates, changed) {
    container.innerHTML = "";
    var expects = step.expect || [];

    if (!expects.length) {
      container.appendChild(el("p", "wizard-warning is-error",
        "No guard. This step will act on whatever screen the terminal is showing."));
    }

    expects.forEach(function (e, i) {
      var row = el("div", "wizard-guard");

      var text = document.createElement("input");
      text.type = "text";
      text.value = e.text;
      text.setAttribute("aria-label", "Guard text " + (i + 1) + " for step " + (stepIndex + 1));
      text.addEventListener("input", function () { e.text = text.value; });
      row.appendChild(text);

      // Typing in these must not rebuild the list: re-rendering under the
      // cursor takes the focus away mid-number, so "12" becomes "1".
      row.appendChild(numberField("row", e.row, 1, 100, function (v) {
        e.row = v;
        refreshPickers();
      }));
      row.appendChild(numberField("col", e.column, 1, 200, function (v) {
        e.column = v;
        refreshPickers();
      }));

      var absent = el("label", "wizard-check");
      var absentBox = document.createElement("input");
      absentBox.type = "checkbox";
      absentBox.checked = !!e.absent;
      absentBox.addEventListener("change", function () {
        e.absent = absentBox.checked;
        changed();
      });
      absent.appendChild(absentBox);
      absent.appendChild(el("span", null, "must NOT be there"));
      absent.title =
        "Inverts the check. This is how a step refuses to read an error line " +
        "such as INVALID ACCOUNT as an answer.";
      row.appendChild(absent);

      var remove = el("button", null, "Remove");
      remove.type = "button";
      remove.addEventListener("click", function () {
        expects.splice(i, 1);
        changed();
      });
      row.appendChild(remove);

      container.appendChild(row);
    });

    var chosen = {};
    expects.forEach(function (e) { chosen[e.row + ":" + e.column + ":" + e.text] = true; });
    var offered = candidates.filter(function (c) {
      return !chosen[c.row + ":" + c.column + ":" + c.text];
    });
    if (offered.length) {
      var shortlist = el("div", "wizard-candidates");
      shortlist.appendChild(el("span", "subtle", "Also on this screen:"));
      offered.forEach(function (c) {
        var button = el("button", "wizard-candidate", c.text.trim());
        button.type = "button";
        button.title = "row " + c.row + ", column " + c.column;
        button.addEventListener("click", function () {
          step.expect = step.expect || [];
          step.expect.push({ row: c.row, column: c.column, text: c.text });
          changed();
        });
        shortlist.appendChild(button);
      });
      container.appendChild(shortlist);
    }

    var add = el("button", "wizard-add", "Add a guard by position");
    add.type = "button";
    add.addEventListener("click", function () {
      step.expect = step.expect || [];
      step.expect.push({ row: 1, column: 1, text: "" });
      changed();
    });
    container.appendChild(add);
  }

  function numberField(labelText, value, min, max, onChange) {
    var label = el("label", "wizard-number");
    label.appendChild(el("span", null, labelText));
    var input = document.createElement("input");
    input.type = "number";
    input.min = String(min);
    input.max = String(max);
    input.value = value;
    input.addEventListener("input", function () {
      var n = parseInt(input.value, 10);
      if (isNaN(n)) {
        return;
      }
      onChange(Math.max(min, Math.min(max, n)));
    });
    label.appendChild(input);
    return label;
  }

  /* ---------------------------------------------------------------- */
  /* Stage 4 — marking the answer                                     */
  /* ---------------------------------------------------------------- */

  function renderAnswer(body) {
    var wrap = el("div", "wizard-section");
    wrap.appendChild(el("h4", null, "The answer"));
    wrap.appendChild(el("p", "subtle",
      "Click a value on the screen below, or drag across it, to mark what the task " +
      "reports back. This is the part a recording cannot work out on its own."));
    body.appendChild(wrap);

    if (!state.finalScreen) {
      body.appendChild(el("p", "task-form-status is-error",
        state.origin === "task"
          ? "The terminal has no screen to show, so the regions below can only be edited as row, column and length."
          : "The screen the flow ended on was not captured, so there is nothing to mark. " +
            "Re-record the flow from the session you want to build the task from."));
    } else {
      var picker = makePicker(state.finalScreen, {
        regions: function () {
          return (state.task.outputs || []).map(function (o, i) {
            return { row: o.row, column: o.column, length: o.length, label: String(i + 1) };
          });
        },
        onSelect: function (row0, col0, length) {
          addOutput(row0, col0, length);
        },
        expandOnClick: true
      });
      pickers.push(picker);
      body.appendChild(picker.element);
    }

    var list = el("div", "wizard-outputs");
    list.setAttribute("data-wizard-outputs", "");
    body.appendChild(list);
    renderOutputList();

    var tools = el("div", "wizard-inline-actions");
    var add = el("button", "wizard-add", "Add a region by position");
    add.type = "button";
    add.addEventListener("click", function () {
      state.task.outputs.push({
        name: uniqueOutputName("value"),
        label: "Value " + (state.task.outputs.length + 1),
        row: 1,
        column: 1,
        length: 10
      });
      renderOutputList();
      refreshPickers();
    });
    tools.appendChild(add);

    var check = el("button", null, "Check what these read");
    check.type = "button";
    check.addEventListener("click", function () {
      runPreview(function () { renderOutputList(); });
    });
    tools.appendChild(check);
    body.appendChild(tools);
  }

  function lineOf(screen, row0) {
    return (screen || "").split("\n")[row0] || "";
  }

  function addOutput(row0, col0, length) {
    var line = lineOf(state.finalScreen, row0);

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
    if (end - col0 > length) {
      length = end - col0;
    }

    var text = line.slice(col0, col0 + length);
    // Name it from the label to the left where there is one, so the result
    // card reads like the screen it came from.
    var left = line.slice(0, col0).replace(/[ .:_-]+$/, "");
    var idx = left.lastIndexOf("  ");
    var suggested = (idx >= 0 ? left.slice(idx + 2) : left).trim();
    if (!suggested || suggested.length > 40 || !/[A-Za-z]/.test(suggested)) {
      // On a list panel there is nothing to the left but the previous
      // column's figure, and "950.00" is not what the value is called. What
      // names it is the column heading above it, which is the only place the
      // screen says what the column is.
      suggested = headingAbove(row0, col0, length) || suggested;
    }
    if (!suggested || suggested.length > 40 || !/[A-Za-z]/.test(suggested)) {
      suggested = "Value " + (state.task.outputs.length + 1);
    }
    state.task.outputs.push({
      name: uniqueOutputName(slug(suggested) || "value"),
      label: suggested,
      row: row0 + 1,
      column: col0 + 1,
      length: length
    });
    renderOutputList();
    refreshPickers();
    status("Marked " + JSON.stringify(text.trim()) + " as " + suggested + ".");
  }

  // headingAbove looks up the screen for a column heading over a region: the
  // nearest row above that has a word overlapping the region's columns.
  // Bounded to a few rows, because past that it stops being a heading and
  // starts being whatever else the panel happens to say.
  function headingAbove(row0, col0, length) {
    var lines = (state.finalScreen || "").split("\n");
    var from = Math.max(0, row0 - 8);
    for (var r = row0 - 1; r >= from; r--) {
      var line = lines[r] || "";
      if (!/[A-Za-z]/.test(line)) {
        // A rule of dashes between the headings and the list is normal, and
        // is not a heading.
        continue;
      }
      var best = "";
      var re = /[A-Za-z][A-Za-z0-9]*(?: [A-Za-z0-9]+)*/g;
      var match;
      while ((match = re.exec(line)) !== null) {
        var start = match.index;
        var end = start + match[0].length - 1;
        // Overlapping, with a little slack: a heading is routinely a
        // character or two off its column, and right-aligned figures sit
        // under a heading that starts to their left.
        if (end >= col0 - 2 && start <= col0 + length + 1) {
          best = match[0].trim();
        }
      }
      if (best && best.length <= 40) {
        return best;
      }
    }
    return "";
  }

  function slug(text) {
    var out = String(text || "")
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "_")
      .replace(/^_+|_+$/g, "");
    if (!out) {
      return "";
    }
    if (!/^[a-z]/.test(out)) {
      out = "v_" + out;
    }
    return out.slice(0, 60).replace(/_+$/, "");
  }

  // Two columns headed the same way are ordinary on a 3270, and two outputs
  // with the same machine name are refused by the server. Resolving it here
  // means the refusal never happens rather than arriving after the work.
  function uniqueOutputName(base, ignoreIndex) {
    var taken = {};
    (state.task.outputs || []).forEach(function (o, i) {
      if (i !== ignoreIndex) {
        taken[o.name] = true;
      }
    });
    var name = base || "value";
    var n = 2;
    while (taken[name]) {
      name = base + "_" + n;
      n++;
    }
    return name;
  }

  function sampleAt(o) {
    if (!state.finalScreen) {
      return "";
    }
    return lineOf(state.finalScreen, o.row - 1).slice(o.column - 1, o.column - 1 + o.length);
  }

  function previewFor(o) {
    if (!state.preview || !state.preview.outputs) {
      return null;
    }
    for (var i = 0; i < state.preview.outputs.length; i++) {
      if (state.preview.outputs[i].name === o.name) {
        return state.preview.outputs[i];
      }
    }
    return null;
  }

  function renderOutputList() {
    var list = modal.querySelector("[data-wizard-outputs]");
    if (!list) {
      return;
    }
    list.innerHTML = "";
    var outputs = state.task.outputs || [];
    if (!outputs.length) {
      list.appendChild(el("p", "subtle",
        "Nothing marked yet. A task with no answer can be saved, but running it can " +
        "only be judged by looking at the terminal."));
      return;
    }

    outputs.forEach(function (o, i) {
      var card = el("div", "wizard-card");

      var head = el("div", "wizard-card-head");
      head.appendChild(el("span", "wizard-region-badge", String(i + 1)));
      var label = document.createElement("input");
      label.type = "text";
      label.className = "wizard-output-label";
      label.value = o.label || o.name;
      label.setAttribute("aria-label", "Label for answer " + (i + 1));
      label.addEventListener("input", function () {
        o.label = label.value;
        o.name = uniqueOutputName(slug(label.value) || "value", i);
      });
      head.appendChild(label);

      var remove = el("button", null, "Remove");
      remove.type = "button";
      remove.addEventListener("click", function () {
        outputs.splice(i, 1);
        renderOutputList();
        refreshPickers();
      });
      head.appendChild(remove);
      card.appendChild(head);

      var region = el("div", "wizard-output-region");
      // These edit the region without rebuilding the list. Re-rendering under
      // the cursor would take the focus away mid-number, so "12" becomes "1"
      // — and the numbers are the whole keyboard path to marking a region.
      var reread = function () {
        card.querySelector("[data-wizard-sample]").textContent = sampleAt(o).trim() || "(blank)";
        refreshPickers();
      };
      region.appendChild(numberField("row", o.row, 1, 100, function (v) {
        o.row = v;
        reread();
      }));
      region.appendChild(numberField("col", o.column, 1, 200, function (v) {
        o.column = v;
        reread();
      }));
      region.appendChild(numberField("width", o.length, 1, 160, function (v) {
        o.length = v;
        reread();
      }));

      var optional = el("label", "wizard-check");
      var optionalBox = document.createElement("input");
      optionalBox.type = "checkbox";
      optionalBox.checked = !!o.optional;
      optionalBox.addEventListener("change", function () { o.optional = optionalBox.checked; });
      optional.appendChild(optionalBox);
      optional.appendChild(el("span", null, "may be missing"));
      optional.title =
        "By default a value the task cannot read is a failed run. A result card " +
        "showing a blank balance is worse than an honest error.";
      region.appendChild(optional);
      card.appendChild(region);

      var reads = el("div", "wizard-output-reads");
      reads.appendChild(el("span", "subtle", "reads"));
      var sample = el("code", "wizard-output-sample", sampleAt(o).trim() || "(blank)");
      sample.setAttribute("data-wizard-sample", "");
      reads.appendChild(sample);
      var checked = previewFor(o);
      if (checked) {
        reads.appendChild(el("span", "subtle", "→"));
        var got = el("code", "wizard-output-sample", checked.found ? checked.value : "not found");
        got.classList.toggle("is-missing", !checked.found);
        reads.appendChild(got);
      }
      card.appendChild(reads);

      var pattern = textField("Extract with (regular expression, optional)", o.pattern || "", 200,
        "([\\d,]+\\.\\d{2})", function (v) { o.pattern = v; });
      pattern.classList.add("wizard-output-pattern");
      card.appendChild(pattern);
      card.appendChild(el("p", "subtle",
        "With one capture group the group is the value, otherwise the whole match is. " +
        "This is what turns \"Cleared balance:  1,240.55 CR\" into \"1,240.55\"."));

      list.appendChild(card);
    });

    var note = el("p", "subtle",
      "A marked region runs to the next text on its row, so a longer value than the " +
      "recorded one is not cut short. Trailing spaces are trimmed from the result.");
    list.appendChild(note);
  }

  /* ---------------------------------------------------------------- */
  /* The screen picker                                                */
  /* ---------------------------------------------------------------- */

  // The same drag gesture as block copy, over a static screen. Reusing
  // screenGrid.metricsFor and pointToCell keeps one definition of "which cell
  // is under the pointer" rather than a second that drifts from it.
  //
  // Every region already marked stays drawn on the screen, numbered to match
  // its card. Marking four values on a dense screen without that is guesswork
  // about which one you already have.
  function makePicker(screenText, opts) {
    var size = screenSize(screenText);
    var holder = el("div", "wizard-screen");
    holder.setAttribute("data-wizard-screen", "");
    var pre = document.createElement("pre");
    pre.textContent = screenText;
    holder.appendChild(pre);
    var overlay = el("div", "wizard-regions");
    holder.appendChild(overlay);
    var marquee = el("div", "wizard-marquee");
    marquee.hidden = true;
    holder.appendChild(marquee);

    var dragAnchor = null;
    var dragRect = null;
    var moved = false;

    var metricsNow = function () {
      return grid() ? grid().metricsFor(pre, size.rows, size.cols) : null;
    };

    var box = function (node, rect) {
      var m = metricsNow();
      if (!m) {
        return false;
      }
      var holderRect = holder.getBoundingClientRect();
      node.style.left = m.rect.left - holderRect.left + rect.left * m.cellW + "px";
      node.style.top = m.rect.top - holderRect.top + rect.top * m.cellH + "px";
      node.style.width = (rect.right - rect.left + 1) * m.cellW + "px";
      node.style.height = (rect.bottom - rect.top + 1) * m.cellH + "px";
      return true;
    };

    var refresh = function () {
      overlay.innerHTML = "";
      var regions = typeof opts.regions === "function" ? opts.regions() : [];
      regions.forEach(function (r) {
        if (!r.row || !r.column || !r.length) {
          return;
        }
        var node = el("div", "wizard-region");
        node.classList.toggle("is-absent", !!r.absent);
        if (r.label) {
          node.appendChild(el("span", "wizard-region-badge", r.label));
        }
        overlay.appendChild(node);
        box(node, {
          top: r.row - 1,
          bottom: r.row - 1,
          left: r.column - 1,
          right: r.column - 1 + r.length - 1
        });
      });
    };

    holder.addEventListener("pointerdown", function (event) {
      var m = metricsNow();
      if (!m) {
        return;
      }
      event.preventDefault();
      dragAnchor = grid().pointToCell(m, event.clientX, event.clientY);
      dragRect = rectFrom(dragAnchor, dragAnchor);
      moved = false;
      marquee.hidden = false;
      box(marquee, dragRect);
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
      var cell = grid().pointToCell(m, event.clientX, event.clientY);
      if (cell.col !== dragAnchor.col || cell.row !== dragAnchor.row) {
        moved = true;
      }
      dragRect = rectFrom(dragAnchor, cell);
      box(marquee, dragRect);
    });

    var finish = function () {
      if (!dragAnchor || !dragRect) {
        return;
      }
      var row = dragRect.top;
      var left = dragRect.left;
      var length = dragRect.right - dragRect.left + 1;
      if (!moved && opts.expandOnClick) {
        // A click, not a drag. Take the whole run of text under the pointer:
        // clicking the middle of a value and getting one character out of the
        // middle of it is never what anybody meant.
        var word = wordAt(lineOf(screenText, row), left);
        left = word.start;
        length = word.length;
      }
      dragAnchor = null;
      dragRect = null;
      marquee.hidden = true;
      if (typeof opts.onSelect === "function") {
        opts.onSelect(row, left, length);
      }
    };
    holder.addEventListener("pointerup", finish);
    holder.addEventListener("pointercancel", function () {
      dragAnchor = null;
      dragRect = null;
      marquee.hidden = true;
    });

    // A rAF after render is not enough: the 3270 font can still be loading,
    // and a screen laid out in a fallback font puts every overlay a fraction
    // of a cell out — or, when the block has no geometry yet, nowhere at all.
    // Watching the block itself covers the font arriving, the modal being
    // resized and the body reflowing, without any of them having to know
    // that overlays exist.
    if (typeof ResizeObserver === "function") {
      var observer = new ResizeObserver(function () { refresh(); });
      observer.observe(pre);
    }
    if (document.fonts && document.fonts.ready && document.fonts.ready.then) {
      document.fonts.ready.then(function () { refresh(); });
    }

    return { element: holder, refresh: refresh };
  }

  function screenSize(screenText) {
    var lines = (screenText || "").split("\n");
    var cols = 0;
    lines.forEach(function (l) {
      if (l.length > cols) {
        cols = l.length;
      }
    });
    return { rows: lines.length, cols: cols || 80 };
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

  // wordAt returns the run of non-space characters containing col, or the
  // single cell when it is blank.
  function wordAt(line, col) {
    if (line.charAt(col) === " " || col >= line.length) {
      return { start: col, length: 1 };
    }
    var start = col;
    while (start > 0 && line.charAt(start - 1) !== " ") {
      start--;
    }
    var end = col;
    while (end < line.length - 1 && line.charAt(end + 1) !== " ") {
      end++;
    }
    return { start: start, length: end - start + 1 };
  }

  function refreshPickers() {
    pickers.forEach(function (p) { p.refresh(); });
  }

  /* ---------------------------------------------------------------- */
  /* Stage 5 — review                                                 */
  /* ---------------------------------------------------------------- */

  function renderReview(body) {
    var wrap = el("div", "wizard-section");
    wrap.appendChild(el("h4", null, "Review"));
    wrap.appendChild(el("p", "subtle",
      "What the server makes of this task, checked with the same validation and the " +
      "same extraction the runner uses."));
    body.appendChild(wrap);

    var summary = el("div", "wizard-review", null);
    summary.setAttribute("data-wizard-review", "");
    body.appendChild(summary);
    renderReviewBody();
    runPreview(renderReviewBody);
  }

  function renderReviewBody() {
    var host = modal && modal.querySelector("[data-wizard-review]");
    if (!host) {
      return;
    }
    host.innerHTML = "";

    var task = collect();
    var head = el("div", "wizard-review-head");
    head.appendChild(el("strong", null, task.name || "(unnamed task)"));
    if (task.description) {
      head.appendChild(el("span", "subtle", task.description));
    }
    host.appendChild(head);

    var fixedCount = Object.keys(state.modes).filter(function (name) {
      return state.modes[name] === "fixed";
    }).length;
    var facts = el("ul", "wizard-facts");
    facts.appendChild(el("li", null,
      (task.parameters || []).length + " input(s) on the form" +
      (fixedCount ? ", " + fixedCount + " value(s) fixed into the steps" : "")));
    facts.appendChild(el("li", null,
      (task.steps || []).length + " step(s), " + (task.steps || []).filter(function (s) {
        return (s.expect || []).length;
      }).length + " guarded"));
    facts.appendChild(el("li", null, (task.outputs || []).length + " value(s) reported back"));
    host.appendChild(facts);

    var preview = state.preview;
    if (!preview) {
      host.appendChild(el("p", "subtle", "Checking…"));
      return;
    }
    if (preview.transportError) {
      host.appendChild(el("p", "wizard-warning is-error",
        "Could not check the task with the server: " + preview.transportError));
      return;
    }
    if (!preview.ok) {
      host.appendChild(el("p", "wizard-warning is-error",
        "This task cannot be saved yet: " + (preview.error || "it is not valid.")));
      return;
    }

    if (preview.replaces) {
      host.appendChild(el("p", "wizard-warning",
        "Saving replaces the task already called " + JSON.stringify(preview.replaces) + "."));
    } else if (state.origin === "task" && state.originalName &&
        state.originalName !== task.name) {
      host.appendChild(el("p", "wizard-warning",
        "The name changed, so this saves as a new task and " +
        JSON.stringify(state.originalName) + " stays in the catalogue."));
    }

    (preview.warnings || []).forEach(function (w) {
      host.appendChild(el("p", "wizard-warning", w));
    });

    if ((preview.outputs || []).length) {
      var title = el("p", "subtle",
        state.origin === "task"
          ? "Read from the screen the terminal is showing now:"
          : "Read from the screen the flow ended on:");
      host.appendChild(title);
      var values = el("dl", "task-result-values");
      preview.outputs.forEach(function (o) {
        values.appendChild(el("dt", null, o.label || o.name));
        var dd = el("dd", null, o.found ? o.value : "not found");
        dd.classList.toggle("is-missing", !o.found);
        values.appendChild(dd);
      });
      host.appendChild(values);
    }
    if ((preview.missing || []).length) {
      host.appendChild(el("p", "wizard-warning is-error",
        "On this screen the task would fail: it could not read " +
        preview.missing.join(", ") + "."));
    }

    host.appendChild(waysToRunIt(task, preview));
  }

  // Where this task can be run from, once it is saved.
  //
  // A task is not only a menu entry: the same document is an MCP tool an
  // assistant can call, an operation on the token-authenticated API, and
  // something an extension can ship to another deployment. All of that has
  // always been true and none of it was ever said at the moment somebody had
  // just built one — so the names had to be worked out from the
  // documentation, which is the same as not having them.
  function waysToRunIt(task, preview) {
    var wrap = document.createElement("details");
    wrap.className = "wizard-uses";
    wrap.open = true;
    var summary = document.createElement("summary");
    summary.textContent = "Where this task can be run from";
    wrap.appendChild(summary);

    var list = el("dl", "wizard-uses-list");

    list.appendChild(el("dt", null, "In the browser"));
    list.appendChild(el("dd", null,
      "Tasks on the menu bar: pick it, fill in the form, read the result card."));

    if (preview.toolName) {
      list.appendChild(el("dt", null, "From an assistant"));
      var tool = el("dd");
      tool.appendChild(document.createTextNode(
        "Over MCP and in the AI chat it is offered as its own tool, "));
      tool.appendChild(copyable(preview.toolName));
      tool.appendChild(document.createTextNode(
        ", with a schema built from the inputs above. A model sees it in the same " +
        "list as every other tool, so it can answer the question rather than " +
        "driving the screens by hand."));
      if (preview.toolNameClash) {
        tool.appendChild(el("p", "wizard-warning is-error",
          "That tool name is already taken by " + JSON.stringify(preview.toolNameClash) +
          ". Tool names drop everything that is not a letter or a digit, so both " +
          "tasks land on one name and a tool-calling client would only ever see one " +
          "of them. Rename this task."));
      }
      list.appendChild(tool);
    }

    list.appendChild(el("dt", null, "From a script or a bot"));
    var api = el("dd");
    api.appendChild(document.createTextNode("Token-authenticated, and it answers with the result:"));
    api.appendChild(curlSnippet(task));
    list.appendChild(api);

    list.appendChild(el("dt", null, "On another deployment"));
    list.appendChild(el("dd", null,
      "The saved task is the export format: GET and POST /api/v1/tasks move it " +
      "between installations, and an extension can ship it alongside the skills " +
      "that explain when to use it."));

    wrap.appendChild(list);
    return wrap;
  }

  function curlSnippet(task) {
    var params = {};
    (task.parameters || []).forEach(function (p) {
      params[p.name] = p.example || (p.sensitive ? "…" : "");
    });
    var body = JSON.stringify({ name: task.name, parameters: params });
    var text =
      "curl -H \"Authorization: Bearer $API_TOKEN\" \\\n" +
      "     -H 'Content-Type: application/json' \\\n" +
      "     -d '" + body + "' \\\n" +
      "     " + window.location.origin + "/api/v1/sessions/$SESSION_ID/tasks/run";
    var pre = el("pre", "wizard-snippet", text);
    pre.appendChild(copyButton(text, "Copy the request"));
    return pre;
  }

  function copyable(text) {
    var code = el("code", "wizard-copyable", text);
    code.appendChild(copyButton(text, "Copy " + text));
    return code;
  }

  function copyButton(text, label) {
    var button = el("button", "wizard-copy", "Copy");
    button.type = "button";
    button.title = label;
    button.addEventListener("click", function () {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(
          function () { notify("Copied.", "success"); },
          function () { notify("Could not copy that.", "error"); }
        );
      } else {
        notify("Could not copy that.", "error");
      }
    });
    return button;
  }

  // runPreview asks the server what this task would do. It is the one check
  // that is not a re-implementation of the server's rules in JavaScript —
  // which is the point of it.
  function runPreview(done) {
    var task = collect();
    return api("/tasks/preview", { task: task, screen: state.finalScreen }).then(
      function (body) {
        state.preview = body;
        if (typeof done === "function") {
          done();
        }
        return body;
      },
      function (err) {
        state.preview = { transportError: (err && err.message) || "request failed" };
        if (typeof done === "function") {
          done();
        }
      }
    );
  }

  /* ---------------------------------------------------------------- */
  /* Saving                                                           */
  /* ---------------------------------------------------------------- */

  // collect projects the wizard's state onto the task document the server
  // validates. "Fixed" parameters are removed and their inputs rewritten to
  // carry the literal, which is what makes the choice real rather than
  // cosmetic.
  function collect() {
    var task = clone(state.task);
    task.name = (task.name || "").trim();
    task.description = (task.description || "").trim();

    var keep = [];
    var fixed = {};
    (task.parameters || []).forEach(function (p) {
      var mode = state.modes[p.name] || "ask";
      if (mode === "fixed") {
        fixed[p.name] = state.fixed[p.name] !== undefined ? state.fixed[p.name] : (p.example || "");
        return;
      }
      if (mode === "secret") {
        p.sensitive = true;
        // A sensitive parameter may not carry a default or an example: both
        // are the secret, written to a catalogue file in clear text.
        p.example = "";
        delete p.default;
      } else {
        p.sensitive = false;
      }
      if (!p.label) {
        delete p.label;
      }
      keep.push(p);
    });
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
      step.expect = (step.expect || []).filter(function (e) {
        return e.text && e.text.trim() !== "";
      });
    });

    task.outputs = (task.outputs || []).map(function (o) {
      var out = {
        name: o.name,
        label: o.label,
        row: o.row,
        column: o.column,
        length: o.length
      };
      if (o.pattern) {
        out.pattern = o.pattern;
      }
      if (o.optional) {
        out.optional = true;
      }
      return out;
    });
    return task;
  }

  function saveTask() {
    if (!state) {
      return;
    }
    var task = collect();
    if (!task.name) {
      status("Give the task a name on the Details stage.", true);
      goTo(0);
      return;
    }
    status("Saving…");
    api("/tasks/save", task).then(
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
      '  <nav class="wizard-stepper" data-wizard-stepper aria-label="Task authoring stages"></nav>',
      '  <div class="wizard-body" data-wizard-body></div>',
      '  <div class="wizard-actions">',
      '    <span class="task-form-status" data-wizard-status aria-live="polite"></span>',
      '    <span class="wizard-nav" data-wizard-nav></span>',
      "  </div>",
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    var closers = modal.querySelectorAll("[data-wizard-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }
    // A resize moves every cell of a rendered screen, and the overlays are
    // absolutely positioned over cells.
    window.addEventListener("resize", function () {
      if (isOpen()) {
        refreshPickers();
      }
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }
    build();
    var trigger = document.querySelector("[data-wizard-open]");
    if (trigger) {
      trigger.addEventListener("click", function () { open(); });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.taskWizard = {
    open: open,
    openTask: function (name) { return open(name); },
    close: close,
    isOpen: isOpen
  };
})();
