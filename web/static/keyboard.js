(function () {
  "use strict";

  var submitting = false;
  var keydownInstalled = false;
  var keySubmitDelayMs = 65;
  var keypadCompactStorageKey = "h3270KeypadCompact";
  var keypadModeStorageKey = "h3270KeypadMode";
  var keypadLayoutStorageKey = "h3270KeypadLayout";
  var keypadScaleRafId = 0;
  var keypadResizeObserver = null;
  var keypadNaturalHeight = 0;
  // The largest share of the display the virtual keyboard may take in focus
  // mode. Focus mode promises the terminal the screen; a third of it is
  // enough for the keys to stay hittable without the promise becoming a lie.
  var FOCUS_KEYPAD_MAX_FRACTION = 0.35;
  var lastKnownCursorRow = null;
  var lastKnownCursorCol = null;
  // Set for one focusin cycle when Ctrl+Tab (or Ctrl+Shift+Tab) deliberately
  // releases keyboard focus from the terminal (see handleKeyDownEvent and
  // installTerminalFocusLock). Without this, the focusin listener that keeps
  // focus pinned to the terminal would immediately snap it back, permanently
  // trapping keyboard users inside the terminal (WCAG 2.1.2).
  var terminalFocusReleasePending = false;
  // True from the moment Ctrl+Tab moves focus to the escape hatch until
  // focus genuinely returns inside .terminal-shell (see installTerminalFocusLock's
  // focusin listener). Read by restoreScreenFocus to avoid dragging focus
  // back into the terminal out from under a deliberate escape while an
  // async screen refresh from an earlier keypress is still in flight.
  var terminalFocusEscaped = false;
  // Escape reflexively means "get me out of here" for most users, but on a
  // 3270 terminal it maps to Clear, which wipes the whole screen. Without a
  // confirmation, one accidental Escape (e.g. muscle memory from closing a
  // dialog) silently destroys unsaved input. clearConfirmArmed tracks
  // whether the PREVIOUS keydown was an unconfirmed Escape; a second Escape
  // within clearConfirmWindowMs actually sends Clear, and any other key (or
  // the window elapsing) cancels the pending confirmation.
  var clearConfirmArmed = false;
  var clearConfirmTimer = 0;
  var clearConfirmWindowMs = 2000;
  var specialKeys = {
    Enter: "Enter",
    BackSpace: "BackSpace",
    Delete: "Delete",
    Insert: "Insert",
    Home: "Home",
    Up: "Up",
    Down: "Down",
    Left: "Left",
    Right: "Right",
    Clear: "Clear",
    Reset: "Reset",
    EraseEOF: "EraseEOF",
    EraseInput: "EraseInput",
    Dup: "Dup",
    FieldMark: "FieldMark",
    SysReq: "SysReq",
    Attn: "Attn",
    NewLine: "NewLine",
    PA1: "PA1",
    PA2: "PA2",
    PA3: "PA3"
  };

  // ---------------------------------------------------------------------
  // Terminal state that lives on the client, not the host.
  //
  // On a real 3270 the cursor, insert/overtype mode and the operator-error
  // lock are all local functions of the terminal — the host learns the
  // cursor position exactly once, in the inbound data stream, when an AID
  // key is pressed. Modelling them here is what removes a network
  // round-trip from every Tab and arrow keypress.
  // ---------------------------------------------------------------------

  // Insert mode (toggled by the Insert key). Off means overtype, which is
  // the 3270 default.
  var insertMode = false;
  // A client-detected operator error (e.g. a letter typed into a numeric
  // field, or overflowing a full field in insert mode). Like a real
  // terminal this inhibits input until Reset. Empty string means no error.
  var operatorError = "";
  // The host's own input-inhibit state, as last reported by the server.
  var hostIndicator = "";
  var hostExplanation = "";
  // Set while an AID key is in flight so the OIA can show X SYSTEM
  // immediately rather than waiting for the next server poll to say so.
  var awaitingHost = false;
  // Keystrokes entered while the host held the keyboard. Real terminals
  // buffer these; dropping them (the previous behaviour) punishes exactly
  // the fast operators who type ahead by reflex.
  var typeAhead = [];
  var typeAheadLimit = 64;
  // Where the cursor is when it is resting somewhere that is not an input
  // field — see the "Cursor over protected text" section. null means the
  // cursor is in a field and the DOM caret is the truth.
  var freeCursor = null;
  // Set for one focusin cycle while keepKeystrokeSink is deliberately putting
  // DOM focus back on a field with the cursor still out on protected text.
  var sinkFocusPending = false;

  var numericCharPattern = /^[0-9.,\-+]$/;

  function findForm(formId) {
    var form = null;
    if (formId) {
      form = document.getElementById(formId) || document.forms[formId];
    }
    if (!form) {
      form = document.querySelector("form.renderer-form");
    }
    if (!form && document.forms.length > 0) {
      form = document.forms[0];
    }
    return form;
  }

  function setCursorInputs(form, row, col) {
    if (!form) {
      return;
    }
    var rowInput = form.querySelector('input[name="cursor_row"]');
    var colInput = form.querySelector('input[name="cursor_col"]');
    if (!rowInput || !colInput) {
      return;
    }
    rowInput.value = String(row);
    colInput.value = String(col);
  }

  function clearCursorInputs(form) {
    if (!form) {
      return;
    }
    var rowInput = form.querySelector('input[name="cursor_row"]');
    var colInput = form.querySelector('input[name="cursor_col"]');
    if (rowInput) {
      rowInput.value = "";
    }
    if (colInput) {
      colInput.value = "";
    }
  }

  function getLineOffsetFromName(name) {
    if (!name) {
      return 0;
    }
    var match = name.match(/^field_\d+_\d+_(\d+)$/);
    if (!match) {
      return 0;
    }
    var value = parseInt(match[1], 10);
    return isNaN(value) ? 0 : value;
  }

  function setCursorFromTarget(form, target) {
    if (!isScreenInput(target)) {
      return;
    }
    if (typeof target.selectionStart !== "number") {
      return;
    }
    var pos = getFieldPosition(target);
    if (!pos) {
      return;
    }
    var lineOffset = getLineOffsetFromName(target.name || "");
    // pos.x (field.StartX) is already the field's first character column,
    // 0-based and directly comparable to the host's reported cursor column
    // (see findInputAtCursor) — it is not an attribute-byte position that
    // needs a +1 to reach the first character cell. Adding one here shifted
    // every reported cursor column one past where the caret actually was,
    // which on a field's last character produced a column matching nothing
    // (or the wrong field) once findInputAtCursor tried to map it back.
    var inputStartX = pos.x;
    if (lineOffset > 0) {
      inputStartX = 0;
    }
    var col = inputStartX + target.selectionStart;
    if (col < 0) {
      col = 0;
    }
    setCursorInputs(form, pos.y, col);
  }

  function sendFormWithKey(key, formId, target) {
    if (submitting) {
      return;
    }
    var form = findForm(formId);
    if (!form) {
      return;
    }
    if (freeCursor && !isCursorNavigationKey(key)) {
      // The whole point of a cursor that can leave the fields: this is the one
      // moment the host is told where it is, and for a "put the cursor beside
      // your choice" screen it is the entire input.
      setCursorInputs(form, freeCursor.row, freeCursor.col);
    } else if (target && !isCursorNavigationKey(key)) {
      setCursorFromTarget(form, target);
    } else if (isCursorNavigationKey(key)) {
      // Navigation should be relative to host cursor, not client-side input focus.
      clearCursorInputs(form);
    }
    var keyInput = form.querySelector('input[name="key"]');
    if (!keyInput) {
      return;
    }
    animateVirtualKey(key);
    keyInput.value = key;
    var preferredFieldName = target && typeof target.name === "string" ? target.name : "";
    var preferredCaret = target && typeof target.selectionStart === "number" ? target.selectionStart : null;
    submitting = true;
    // Show X SYSTEM the instant the key goes out. Waiting for the next
    // server poll to report the lock would leave a visible gap in which the
    // terminal looks idle while the host is in fact thinking — the exact
    // ambiguity the OIA exists to remove.
    awaitingHost = true;
    renderOIA();
    window.setTimeout(function () {
      var request;
      try {
        request = submitFormWithoutNavigation(form, formId, preferredFieldName, preferredCaret);
      } catch (err) {
        submitting = false;
        form.submit();
        return;
      }
      if (!request || typeof request.then !== "function") {
        submitting = false;
        finishHostRoundTrip();
        return;
      }
      request.then(
        function () {
          submitting = false;
          finishHostRoundTrip();
        },
        function () {
          submitting = false;
          finishHostRoundTrip();
        }
      );
    }, keySubmitDelayMs);
  }

  // The OIA status line (keyboard lock, model, dimensions, cursor) is
  // rendered once server-side at full page load and marked aria-live, but
  // nothing ever updated it afterward — every subsequent async refresh only
  // ever replaced .screen-container's innerHTML, which the status line
  // lives outside of. /screen/content now includes these fields so this can
  // keep it live.
  function updateScreenStatusLine(payload) {
    if (!payload) {
      return;
    }
    var fields = [
      ["[data-status-keyboard]", payload.statusKeyboard],
      ["[data-status-model]", payload.statusModel],
      ["[data-status-dimensions]", payload.statusDimensions],
      ["[data-status-cursor]", payload.statusCursor],
      ["[data-oia-online]", payload.oiaOnline]
    ];
    for (var i = 0; i < fields.length; i++) {
      var selector = fields[i][0];
      var value = fields[i][1];
      if (typeof value !== "string") {
        continue;
      }
      var el = document.querySelector(selector);
      if (el) {
        el.textContent = value;
      }
    }
    if (typeof payload.oiaIndicator === "string") {
      hostIndicator = payload.oiaIndicator;
      hostExplanation =
        typeof payload.oiaExplanation === "string" ? payload.oiaExplanation : "";
      // An authoritative reading from the host supersedes the optimistic
      // "waiting" state the client set when it sent the AID key.
      awaitingHost = false;
    }
    renderOIA();

    // A host that hangs up does not necessarily produce a 401 on the next
    // request — s3270 stays alive and simply reports "not connected" — so
    // watch the OIA for it and hand off to the reconnect logic. Without
    // this, a drop mid-session just looks like a terminal that stopped
    // responding.
    if (
      payload.oiaConnected === false &&
      window.ThreeSeventyWeb &&
      typeof window.ThreeSeventyWeb.notifySessionExpired === "function"
    ) {
      window.ThreeSeventyWeb.notifySessionExpired();
    }
  }

  // renderOIA paints the Operator Information Area. Precedence matters and
  // mirrors a real terminal: an operator error outranks a system wait,
  // because pressing Enter harder will not clear it — only Reset will.
  function renderOIA() {
    var indicator = "";
    var explanation = "";
    var kind = "ready";

    if (operatorError) {
      indicator = "X -f";
      explanation = operatorError + " — press Reset (or Esc) to continue";
      kind = "error";
    } else if (hostIndicator === "X -f") {
      indicator = hostIndicator;
      explanation = hostExplanation || "Operator error — press Reset";
      kind = "error";
    } else if (awaitingHost || hostIndicator) {
      indicator = hostIndicator || "X SYSTEM";
      explanation = hostExplanation || "Host is processing — input inhibited";
      kind = "wait";
    } else {
      explanation = "Ready for input";
    }

    var indicatorEl = document.querySelector("[data-oia-indicator]");
    if (indicatorEl) {
      indicatorEl.textContent = indicator;
      indicatorEl.setAttribute("data-oia-state", kind);
      if (explanation) {
        indicatorEl.setAttribute("title", explanation);
      } else {
        indicatorEl.removeAttribute("title");
      }
    }

    // Only the inhibit explanation is announced. The visual bar also carries
    // the cursor position, and putting aria-live on the whole bar (as it was)
    // made a screen reader read out row/column on every single keystroke.
    var announce = document.querySelector("[data-oia-announce]");
    if (announce && announce.textContent !== explanation) {
      announce.textContent = explanation;
    }

    var insertEl = document.querySelector("[data-oia-insert]");
    if (insertEl) {
      insertEl.hidden = !insertMode;
    }

    var shell = getTerminalShell();
    if (shell) {
      shell.setAttribute("data-oia-state", kind);
    }
  }

  function setOperatorError(reason) {
    if (operatorError === reason) {
      return;
    }
    operatorError = reason;
    renderOIA();
  }

  // clearOperatorError resolves whichever inhibit is actually in force: a
  // client-side one is purely local, but a host-reported error needs the
  // real Reset AID sent upstream. Returns true if it handled the situation.
  function clearOperatorError(formId) {
    if (operatorError) {
      operatorError = "";
      renderOIA();
      return true;
    }
    if (hostIndicator === "X -f") {
      sendFormWithKey(specialKeys.Reset, formId, document.activeElement);
      return true;
    }
    return false;
  }

  function setInsertMode(next) {
    insertMode = !!next;
    renderOIA();
  }

  function submitFormWithoutNavigation(form, formId, preferredFieldName, preferredCaret) {
    var action = "/submit/async";
    var method = (form.getAttribute("method") || "post").toUpperCase();
    var body = new URLSearchParams(new FormData(form));

    return fetch(action, {
      method: method,
      headers: {
        "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        Accept: "text/html,application/xhtml+xml"
      },
      credentials: "same-origin",
      body: body.toString()
    })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("submit failed");
        }
        return fetch("/screen/content", {
          headers: {
            Accept: "application/json",
            "Cache-Control": "no-cache"
          },
          credentials: "same-origin"
        });
      })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("content refresh failed");
        }
        return response.json();
      })
      .then(function (payload) {
        if (!payload || typeof payload.html !== "string") {
          return;
        }
        if (
          typeof payload.cursorRow === "number" &&
          isFinite(payload.cursorRow) &&
          payload.cursorRow >= 0 &&
          typeof payload.cursorCol === "number" &&
          isFinite(payload.cursorCol) &&
          payload.cursorCol >= 0
        ) {
          lastKnownCursorRow = payload.cursorRow;
          lastKnownCursorCol = payload.cursorCol;
        }
        var container = document.querySelector(".screen-container");
        if (!container) {
          return;
        }
        container.innerHTML = payload.html;
        updateScreenStatusLine(payload);

        var updatedForm = container.querySelector("form.renderer-form");
        var updatedFormId = updatedForm ? (updatedForm.id || updatedForm.getAttribute("name")) : formId;
        if (typeof window.installKeyHandler === "function") {
          window.installKeyHandler(updatedFormId);
        }
        restoreScreenFocus(
          updatedForm,
          preferredFieldName,
          preferredCaret,
          payload.cursorRow,
          payload.cursorCol
        );
        if (typeof window.sizeScreenContainer === "function") {
          window.sizeScreenContainer();
        }
      })
      .catch(function () {
        // Fall back to full form submit if async update fails.
        form.submit();
      });
  }

  function isCursorNavigationKey(key) {
    if (!key) {
      return false;
    }
    var upper = String(key).trim().toUpperCase();
    return (
      upper === "TAB" ||
      upper === "BACKTAB" ||
      upper === "UP" ||
      upper === "DOWN" ||
      upper === "LEFT" ||
      upper === "RIGHT" ||
      upper === "HOME"
    );
  }

  function restoreScreenFocus(form, preferredFieldName, preferredCaret, cursorRow, cursorCol) {
    if (!form) {
      return;
    }
    // A key press (e.g. Tab) can trigger this async screen refresh, and by
    // the time it resolves the user may have since deliberately left the
    // terminal via the Ctrl+Tab escape hatch. Don't drag focus back in out
    // from under that — see terminalFocusEscaped. (Checking
    // document.activeElement here instead would misfire: replacing
    // .screen-container's innerHTML just above transiently blurs focus to
    // <body> on every refresh, escape or not.)
    if (terminalFocusEscaped) {
      return;
    }

    // A new screen means the host has set the cursor, so whatever the operator
    // had done locally is superseded. Replacing the container's innerHTML has
    // already destroyed the overlay caret; this drops the state behind it.
    clearFreeCursor();

    var target = null;
    var caret = null;
    var hasCursor =
      typeof cursorRow === "number" &&
      isFinite(cursorRow) &&
      cursorRow >= 0 &&
      typeof cursorCol === "number" &&
      isFinite(cursorCol) &&
      cursorCol >= 0;

    if (hasCursor) {
      var located = findInputAtCursor(form, cursorRow, cursorCol);
      if (located && located.input) {
        target = located.input;
        caret = located.caret;
      } else {
        // The host parked its cursor on protected text — a menu saying "the
        // cursor is here, press Enter". That used to be discarded and the
        // caret dumped into the first field, silently moving the cursor the
        // application had deliberately placed. Now it is honoured.
        setFreeCursor(form, cursorRow, cursorCol);
      }
    }

    if (preferredFieldName && form.elements && form.elements[preferredFieldName]) {
      if (!target) {
        target = form.elements[preferredFieldName];
      }
    }
    if (!target) {
      target =
        form.querySelector("input[data-x][data-y]") ||
        form.querySelector("textarea.unformatted");
    }
    if (!target || typeof target.focus !== "function") {
      return;
    }

    target.focus();
    if (
      caret === null &&
      typeof preferredCaret === "number" &&
      typeof target.setSelectionRange === "function" &&
      typeof target.value === "string"
    ) {
      caret = preferredCaret;
    }
    if (
      caret !== null &&
      typeof target.setSelectionRange === "function" &&
      typeof target.value === "string"
    ) {
      if (caret < 0) {
        caret = 0;
      }
      if (caret > target.value.length) {
        caret = target.value.length;
      }
      target.setSelectionRange(caret, caret);
    }
  }

  function focusTerminalInput() {
    var form = findForm();
    if (!form) {
      return false;
    }
    var active = document.activeElement;
    if (active && form.contains(active) && isEditableTarget(active)) {
      return true;
    }

    var target = null;
    var caret = null;
    if (
      typeof lastKnownCursorRow === "number" &&
      isFinite(lastKnownCursorRow) &&
      lastKnownCursorRow >= 0 &&
      typeof lastKnownCursorCol === "number" &&
      isFinite(lastKnownCursorCol) &&
      lastKnownCursorCol >= 0
    ) {
      var located = findInputAtCursor(form, lastKnownCursorRow, lastKnownCursorCol);
      if (located && located.input) {
        target = located.input;
        caret = located.caret;
      }
    }

    if (!target) {
      target =
        form.querySelector("input[data-x][data-y]:not([disabled]):not([readonly])") ||
        form.querySelector("textarea.unformatted");
    }
    if (!target || typeof target.focus !== "function") {
      return false;
    }

    target.focus();
    if (
      caret !== null &&
      typeof target.setSelectionRange === "function" &&
      typeof target.value === "string"
    ) {
      var clamped = caret;
      if (clamped < 0) {
        clamped = 0;
      }
      if (clamped > target.value.length) {
        clamped = target.value.length;
      }
      target.setSelectionRange(clamped, clamped);
    }
    return true;
  }

  function focusTerminalShell() {
    var shell = getTerminalShell();
    if (!shell || typeof shell.focus !== "function") {
      return false;
    }
    if (!shell.hasAttribute("tabindex")) {
      shell.setAttribute("tabindex", "-1");
    }
    shell.focus();
    return document.activeElement === shell;
  }

  function focusTerminalContext() {
    if (focusTerminalInput()) {
      return true;
    }
    return focusTerminalShell();
  }

  function isModalOpen() {
    var selectors = [
      "[data-settings-modal]",
      "[data-logs-modal]",
      "[data-disconnect-modal]",
      "[data-about-modal]",
      "[data-chaos-runs-modal]",
      "[data-chaos-hints-modal]",
      "[data-chaos-map-modal]",
      "[data-chaos-flow-modal]",
      "[data-chaos-flow-screen-hints-modal]",
      "[data-chaos-report-modal]",
      "[data-chaos-start-log-confirm-modal]",
      "[data-chaos-confirm-modal]",
      "[data-modal]",
      "[data-copilot-clear-modal]",
      "[data-copilot-modal]",
      "[data-history-modal]",
      // These three were missing, so a keystroke landing on a button inside
      // them (rather than a text input, which isEditableTarget already
      // covers) was still reaching the host — pressing F3 to dismiss the
      // transfer dialog would fire PF3 at the application behind it.
      "[data-transfer-modal]",
      "[data-profiles-modal]",
      // The keymap editor especially: this handler is on window, so its
      // capture listener runs before the editor's document one, and without
      // this the key being recorded would also be sent to the host.
      "[data-keymap-modal]",
      "[data-host-details-modal]",
      // The AI provider dialog, the Copilot device-login dialog, the task
      // runner and the task wizard. All four were missing.
      //
      // The AI provider one was the worst of them, and the symptom was not the
      // stray-keystroke problem this list is mostly about. With the dialog open
      // but unrecognised, shouldKeepTerminalFocus() saw an ordinary <select> on
      // an ordinary page and did what it does there: preventDefault() on
      // pointerdown, then focus back to the terminal on click. On a phone that
      // is "tapping the provider dropdown does nothing, and the software
      // keyboard opens instead" — preventDefault stops the native picker from
      // ever opening, and the focus that lands in a screen field is what raises
      // the keyboard. Every control in the dialog was affected; the dropdown is
      // just where it was noticed.
      "[data-ai-modal]",
      "[data-tasks-modal]",
      "[data-wizard-modal]"
    ];
    for (var i = 0; i < selectors.length; i++) {
      var el = document.querySelector(selectors[i]);
      if (el && !el.hidden) {
        return true;
      }
    }
    // And then the general case, because the list above is the bug.
    //
    // Every dialog has to be added to it by hand, nothing fails when one is
    // not, and the consequence is invisible until somebody taps a control that
    // stops working. Four had been missed. A dialog that says it is a modal —
    // role="dialog" with aria-modal="true", which is how a screen reader is
    // told the same thing — is one, whether or not anybody remembered this
    // list. The explicit names stay: they are a cheap fast path, and a couple
    // of the older dialogs do not carry the attributes.
    var dialogs = document.querySelectorAll('[role="dialog"][aria-modal="true"]');
    for (var j = 0; j < dialogs.length; j++) {
      if (isRendered(dialogs[j])) {
        return true;
      }
    }
    // An open drop-down in the menu bar is not a dialog, but for this
    // handler's purposes it wants the same answer. While a menu is open the
    // arrows walk its items, Enter runs the one under the cursor and a letter
    // jumps to the item starting with it — every one of which this handler
    // would otherwise have sent to the host as an AID key or a keystroke. The
    // focus lock has to stand down for the same reason: it exists to keep
    // typing in the terminal, and an open menu is the one time typing is not
    // meant for the terminal.
    if (document.querySelector("[data-app-menu].is-open")) {
      return true;
    }
    return false;
  }

  // isRendered reports whether an element occupies space on the page. Dialogs
  // here are dismissed in several different ways — the hidden attribute, a
  // display:none class, an unmounted parent — so asking the layout is the only
  // answer that covers all of them.
  function isRendered(el) {
    if (!el) {
      return false;
    }
    if (typeof el.getClientRects !== "function") {
      return false;
    }
    return el.getClientRects().length > 0;
  }

  function getTerminalShell() {
    return document.querySelector(".terminal-shell");
  }

  function isInsideTerminalShell(target) {
    if (!target || typeof target.closest !== "function") {
      return false;
    }
    var shell = getTerminalShell();
    if (!shell) {
      return false;
    }
    return !!target.closest(".terminal-shell");
  }

  function shouldKeepTerminalFocus(target) {
    if (!target || typeof target.closest !== "function") {
      return false;
    }
    if (isModalOpen()) {
      return false;
    }
    if (isInsideTerminalShell(target)) {
      return false;
    }
    if (target.closest("[data-terminal-size-slider]")) {
      return false;
    }
    if (target.closest("#copilot-panel")) {
      return false;
    }
    if (target.closest("[data-terminal-escape-hatch]")) {
      return false;
    }
    // A menu trigger has to take the focus it is clicked with, or the menu it
    // opens has nothing to arrow away from.
    if (target.closest("[data-app-menu]")) {
      return false;
    }
    if (target.closest("[data-terminal-controls]")) {
      return true;
    }
    var control = target.closest(
      "button, a[href], [role='button'], input, select, textarea, [tabindex]"
    );
    if (!control) {
      return false;
    }
    if (control.matches('input[type="range"]')) {
      return false;
    }
    return true;
  }

  function shouldPreventPointerDefaultForFocusLock(target) {
    if (!shouldKeepTerminalFocus(target)) {
      return false;
    }
    // Allow terminal size interactions (steppers, slider, fit, reset) to run
    // normally; focus is restored on click handler.
    if (target.closest("[data-terminal-controls]")) {
      return false;
    }
    return true;
  }

  // Timestamp of the most recent pointerdown anywhere in the document. The
  // focusin listener below uses this to tell a mouse-driven focus change
  // (where snapping focus back to the terminal is a helpful convenience)
  // apart from a keyboard Tab-driven one (where it would recreate the
  // keyboard trap described at installTerminalFocusLock's focusin handler).
  var lastPointerDownAt = 0;

  function installTerminalFocusLock() {
    document.addEventListener(
      "pointerdown",
      function (event) {
        lastPointerDownAt = Date.now();
        if (!shouldPreventPointerDefaultForFocusLock(event.target)) {
          return;
        }
        event.preventDefault();
      },
      true
    );

    document.addEventListener(
      "click",
      function (event) {
        if (!shouldKeepTerminalFocus(event.target)) {
          return;
        }
        window.requestAnimationFrame(function () {
          if (isModalOpen()) {
            return;
          }
          focusTerminalContext();
        });
      },
      true
    );

    document.addEventListener(
      "focusin",
      function (event) {
        if (terminalFocusReleasePending) {
          terminalFocusReleasePending = false;
          return;
        }
        if (isModalOpen()) {
          return;
        }
        if (isInsideTerminalShell(event.target)) {
          terminalFocusEscaped = false;
          return;
        }
        // Only auto-return focus to the terminal for a MOUSE/touch-driven
        // focus change (e.g. clicking a toolbar button should leave the
        // terminal ready to keep receiving keystrokes). A focus change with
        // no recent pointerdown is keyboard (Tab) navigation, and forcing
        // focus back to the terminal here would trap keyboard users on
        // every toolbar control they reach (WCAG 2.1.2).
        if (Date.now() - lastPointerDownAt > 150) {
          return;
        }
        if (event.target.closest("[data-terminal-size-slider]")) {
          return;
        }
        if (event.target.closest("[data-terminal-controls], [data-app-menu]")) {
          return;
        }
        if (event.target.matches && event.target.matches('input[type="range"]')) {
          return;
        }
        if (event.target.closest("#copilot-panel")) {
          return;
        }
        if (event.target.closest("[data-terminal-escape-hatch]")) {
          return;
        }
        window.requestAnimationFrame(function () {
          focusTerminalContext();
        });
      },
      true
    );
  }

  function findInputAtCursor(form, row, col) {
    if (!form) {
      return null;
    }
    var nodes = form.querySelectorAll("input[data-x][data-y]");
    for (var i = 0; i < nodes.length; i++) {
      var input = nodes[i];
      if (!isScreenInput(input) || input.disabled || input.readOnly) {
        continue;
      }
      var x = parseInt(input.dataset.x, 10);
      var y = parseInt(input.dataset.y, 10);
      if (isNaN(x) || isNaN(y) || y !== row) {
        continue;
      }
      var width = parseInt(input.dataset.w, 10);
      if (isNaN(width) || width <= 0) {
        width = input.maxLength || (input.value ? input.value.length : 1);
      }
      if (col < x || col >= x+width) {
        continue;
      }
      return {
        input: input,
        caret: col - x
      };
    }
    return null;
  }

  function insertTextIntoFocusedInput(text) {
    var target = document.activeElement;
    if (!isEditableTarget(target) || target.disabled || target.readOnly) {
      return false;
    }
    if (target.dataset && target.dataset.numeric === "1" && /[^0-9.,\-+]/.test(text)) {
      setOperatorError("Numeric field");
      return false;
    }
    var value = target.value || "";
    var start = typeof target.selectionStart === "number" ? target.selectionStart : value.length;
    var end = typeof target.selectionEnd === "number" ? target.selectionEnd : start;
    if (start > end) {
      var temp = start;
      start = end;
      end = temp;
    }
    var next = value.slice(0, start) + text + value.slice(end);
    if (typeof target.maxLength === "number" && target.maxLength > 0 && next.length > target.maxLength) {
      return false;
    }
    target.value = next;
    var caret = start + text.length;
    if (typeof target.setSelectionRange === "function") {
      target.setSelectionRange(caret, caret);
    }
    target.dispatchEvent(new Event("input", { bubbles: true }));
    var form = findForm();
    if (form) {
      setCursorFromTarget(form, target);
    }
    return true;
  }

  function mapFunctionKey(event) {
    if (event.metaKey || event.ctrlKey) {
      return "";
    }
    var key = event.key;
    if (key && key[0] === "F") {
      var n = parseInt(key.substring(1), 10);
      if (!isNaN(n)) {
        if (n >= 1 && n <= 12) {
          if (event.shiftKey) {
            n += 12;
          }
          return "PF" + n;
        }
        if (n >= 13 && n <= 24) {
          return "PF" + n;
        }
      }
    }

    var code = event.keyCode || event.which;
    if (code >= 112 && code <= 123) {
      var idx = code - 111;
      if (event.shiftKey) {
        idx += 12;
      }
      return "PF" + idx;
    }

    return "";
  }

  function mapSpecialKey(event) {
    var code = event.keyCode || event.which;

    if (event.key === "Enter" || code === 13) {
      return specialKeys.Enter;
    }
    if (event.key === "Backspace" || code === 8) {
      return specialKeys.BackSpace;
    }
    if (event.key === "Delete" || code === 46) {
      return specialKeys.Delete;
    }
    if (event.key === "Insert" || code === 45) {
      return specialKeys.Insert;
    }
    if (event.key === "Home" || code === 36) {
      return specialKeys.Home;
    }
    if (event.key === "ArrowUp" || code === 38) {
      return specialKeys.Up;
    }
    if (event.key === "ArrowDown" || code === 40) {
      return specialKeys.Down;
    }
    if (event.key === "ArrowLeft" || code === 37) {
      return specialKeys.Left;
    }
    if (event.key === "ArrowRight" || code === 39) {
      return specialKeys.Right;
    }
    if (event.key === "Escape" || code === 27) {
      return specialKeys.Clear;
    }
    return "";
  }

  function mapPaKeys(event) {
    if (!event.altKey || event.ctrlKey || event.metaKey) {
      return "";
    }
    if (event.key === "F1" || event.keyCode === 112) {
      return specialKeys.PA1;
    }
    if (event.key === "F2" || event.keyCode === 113) {
      return specialKeys.PA2;
    }
    if (event.key === "F3" || event.keyCode === 114) {
      return specialKeys.PA3;
    }
    return "";
  }

  function mapVisualKey(event) {
    if (!event) {
      return "";
    }
    var special = mapSpecialKey(event);
    if (special) {
      return normalizeVirtualKey(special);
    }
    var pa = mapPaKeys(event);
    if (pa) {
      return normalizeVirtualKey(pa);
    }
    var pf = mapFunctionKey(event);
    if (pf) {
      return normalizeVirtualKey(pf);
    }
    if (event.key === "Tab") {
      return event.shiftKey ? "BACKTAB" : "TAB";
    }
    if (!event.metaKey && !event.ctrlKey && !event.altKey && event.key && event.key.length === 1) {
      if (event.key === " ") {
        return "CHAR_SPACE";
      }
      return "CHAR_" + event.key.toUpperCase();
    }
    return "";
  }

  // resolveCustomBinding asks keymap.js whether this keystroke has a
  // user-defined meaning. Returns "" when it does not, or when the module is
  // absent — the terminal must keep working with its built-in mapping if the
  // keymap script fails to load.
  function resolveCustomBinding(event) {
    if (!window.ThreeSeventyWeb || !window.ThreeSeventyWeb.keymap) {
      return "";
    }
    try {
      return window.ThreeSeventyWeb.keymap.resolve(event) || "";
    } catch (_) {
      return "";
    }
  }

  function isEditableTarget(target) {
    if (!target || !target.tagName) {
      return false;
    }
    var tag = target.tagName.toLowerCase();
    if (tag === "textarea") {
      return true;
    }
    if (tag !== "input") {
      return false;
    }
    var type = (target.type || "").toLowerCase();
    return type !== "button" && type !== "submit" && type !== "checkbox" && type !== "radio";
  }

  function isNativeNavKey(event) {
    var code = event.keyCode || event.which;
    if (event.key === "Tab" || code === 9) {
      return true;
    }
    if (event.key === "ArrowUp" || code === 38) {
      return true;
    }
    if (event.key === "ArrowDown" || code === 40) {
      return true;
    }
    if (event.key === "ArrowLeft" || code === 37) {
      return true;
    }
    if (event.key === "ArrowRight" || code === 39) {
      return true;
    }
    if (event.key === "Backspace" || code === 8) {
      return true;
    }
    if (event.key === "Delete" || code === 46) {
      return true;
    }
    return false;
  }

  function getFieldPosition(el) {
    if (!el || !el.dataset) {
      return null;
    }
    var x = parseInt(el.dataset.x, 10);
    var y = parseInt(el.dataset.y, 10);
    if (isNaN(x) || isNaN(y)) {
      return null;
    }
    return { x: x, y: y };
  }

  function findNearestField(current, direction) {
    var pos = getFieldPosition(current);
    if (!pos) {
      return null;
    }
    var inputs = document.querySelectorAll("input.h3270-input, input.h3270-input-intensified, input.h3270-input-hidden");
    var best = null;
    var bestDy = null;
    for (var i = 0; i < inputs.length; i++) {
      var el = inputs[i];
      if (el === current) {
        continue;
      }
      var p = getFieldPosition(el);
      if (!p) {
        continue;
      }
      var dy = p.y - pos.y;
      if (direction === "up" && dy >= 0) {
        continue;
      }
      if (direction === "down" && dy <= 0) {
        continue;
      }
      var dx = Math.abs(p.x - pos.x);
      var score = Math.abs(dy) * 1000 + dx;
      if (bestDy === null || score < bestDy) {
        bestDy = score;
        best = el;
      }
    }
    return best;
  }

  function isScreenInput(el) {
    if (!isEditableTarget(el)) {
      return false;
    }
    if (!el.dataset || el.dataset.x == null || el.dataset.y == null) {
      return false;
    }
    return true;
  }

  function getOrderedScreenInputs(form) {
    if (!form) {
      return [];
    }
    var nodes = form.querySelectorAll("input[data-x][data-y]");
    var entries = [];
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (!isScreenInput(el) || el.disabled || el.readOnly) {
        continue;
      }
      var pos = getFieldPosition(el);
      if (!pos) {
        continue;
      }
      entries.push({ el: el, x: pos.x, y: pos.y });
    }
    entries.sort(function (a, b) {
      if (a.y !== b.y) {
        return a.y - b.y;
      }
      if (a.x !== b.x) {
        return a.x - b.x;
      }
      return 0;
    });
    var ordered = [];
    for (var j = 0; j < entries.length; j++) {
      ordered.push(entries[j].el);
    }
    return ordered;
  }

  // ---------------------------------------------------------------------
  // Cursor over protected text.
  //
  // On a 3270 the cursor can rest on any cell of the display, protected or
  // not. This rendering only has DOM nodes for the *unprotected* fields, so
  // the caret could previously only ever be inside one of them — whole rows
  // of protected text were skipped over by the arrow keys, and the cursor
  // could not be parked on one.
  //
  // That is not cosmetic. A family of mainframe applications is driven by
  // cursor position over protected text: "put the cursor beside your choice
  // and press Enter" menus, cursor-select fields, and anything that reads the
  // inbound cursor address rather than a field value. Without a cursor that
  // can leave the fields, those screens simply cannot be operated.
  //
  // freeCursor is that cursor. When it is set the DOM caret is no longer the
  // truth: an overlay caret is painted over the cell, the fields' own carets
  // are hidden, the OIA reads the free position, and the next AID key reports
  // it. DOM focus deliberately stays on whichever input had it, because that
  // is what keeps keystrokes arriving at the handler at all — the cell the
  // operator is on and the node the browser delivers keys to are different
  // questions, and conflating them is what would fight the focus lock.
  // ---------------------------------------------------------------------

  function screenDims(form) {
    var rows = form ? parseInt(form.dataset.rows, 10) : NaN;
    var cols = form ? parseInt(form.dataset.cols, 10) : NaN;
    return {
      rows: rows > 0 ? rows : 24,
      cols: cols > 0 ? cols : 80
    };
  }

  // normalizeCell wraps a cell address into the screen, the way a 3270 does:
  // off the right-hand edge continues on the next row, off the bottom
  // continues at the top.
  function normalizeCell(dims, row, col) {
    while (col < 0) {
      col += dims.cols;
      row -= 1;
    }
    while (col >= dims.cols) {
      col -= dims.cols;
      row += 1;
    }
    row = ((row % dims.rows) + dims.rows) % dims.rows;
    return { row: row, col: col };
  }

  function freeCursorOverlay(create) {
    var grid = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
    if (!grid || typeof grid.overlayLayer !== "function") {
      return null;
    }
    var layer = grid.overlayLayer();
    if (!layer) {
      return null;
    }
    var el = layer.querySelector(":scope > .screen-free-cursor");
    if (!el && create) {
      el = document.createElement("div");
      el.className = "screen-free-cursor";
      el.setAttribute("aria-hidden", "true");
      layer.appendChild(el);
    }
    return el;
  }

  // paintFreeCursor draws the overlay caret over freeCursor's cell. It is also
  // the repaint path: the terminal is resizable and the font metrics change
  // with it, so the position is recomputed from the grid rather than cached.
  function paintFreeCursor() {
    var el = freeCursorOverlay(!!freeCursor);
    if (!el) {
      return;
    }
    if (!freeCursor) {
      el.hidden = true;
      return;
    }
    var grid = window.ThreeSeventyWeb.screenGrid;
    var metrics = grid.metrics();
    if (!metrics) {
      el.hidden = true;
      return;
    }
    grid.positionOverCells(el, metrics, {
      top: freeCursor.row,
      bottom: freeCursor.row,
      left: freeCursor.col,
      right: freeCursor.col
    });
    el.hidden = false;
  }

  // The OIA's CURSOR readout is 1-based, matching the server's own format, so
  // that a position read off the screen means the same thing whoever wrote it.
  function showCursorReadout(row, col) {
    var el = document.querySelector("[data-status-cursor]");
    if (el) {
      el.textContent = String(row + 1) + "," + String(col + 1);
    }
  }

  function setFreeCursor(form, row, col) {
    freeCursor = { row: row, col: col };
    var shell = getTerminalShell();
    if (shell) {
      // Drives `caret-color: transparent` on the fields. Two visible carets
      // would be worse than none: the operator has to be able to see where
      // the keyboard is actually pointing.
      shell.setAttribute("data-free-cursor", "1");
    }
    paintFreeCursor();
    // A repaint after layout settles. setFreeCursor is called from the
    // screen-refresh path, where the container has just been replaced and the
    // terminal has not been resized to fit it yet, so the metrics read now can
    // be a frame out of date.
    if (typeof window.requestAnimationFrame === "function") {
      window.requestAnimationFrame(paintFreeCursor);
    }
    showCursorReadout(row, col);
    return true;
  }

  function clearFreeCursor() {
    if (!freeCursor) {
      return false;
    }
    freeCursor = null;
    var shell = getTerminalShell();
    if (shell) {
      shell.removeAttribute("data-free-cursor");
    }
    paintFreeCursor();
    return true;
  }

  // fieldSpan is the cells an input actually occupies on its row. data-w is
  // the field's full width, which is not the same as its value's length —
  // that difference is why leaving a field sideways has to be expressed in
  // cells rather than in caret offsets.
  function fieldSpan(el) {
    var pos = getFieldPosition(el);
    if (!pos) {
      return null;
    }
    var start = getLineOffsetFromName(el.name || "") > 0 ? 0 : pos.x;
    var width = parseInt(el.dataset.w, 10);
    if (isNaN(width) || width <= 0) {
      width = el.maxLength > 0 ? el.maxLength : 1;
    }
    return { row: pos.y, start: start, end: start + width - 1 };
  }

  // moveToCell puts the cursor on a screen cell: into the field if the cell
  // belongs to one, on the free cursor if it does not.
  function moveToCell(form, dims, row, col) {
    var cell = normalizeCell(dims, row, col);
    var hit = findInputAtCursor(form, cell.row, cell.col);
    if (hit && hit.input) {
      clearFreeCursor();
      return placeCaret(hit.input, hit.caret);
    }
    return setFreeCursor(form, cell.row, cell.col);
  }

  // cursorCell is where the cursor is now, in screen coordinates, whichever
  // of the two representations currently holds it.
  function cursorCell(form) {
    if (freeCursor) {
      return { row: freeCursor.row, col: freeCursor.col };
    }
    var current = currentScreenInput(form);
    return current ? absoluteCursorOf(current) : null;
  }

  // inhibitOnProtected is the answer to "the operator typed while the cursor
  // was on protected text". A real terminal raises input-inhibited and waits
  // for Reset rather than quietly moving the character somewhere it fits.
  function inhibitOnProtected() {
    if (!freeCursor) {
      return false;
    }
    setOperatorError("Protected field");
    return true;
  }

  // installFreeCursorPointer makes clicking protected text park the cursor
  // there, which is how anyone would expect to answer a "position the cursor
  // beside your choice" screen with a mouse. Clicking a field is left to the
  // browser: focusing the input is already the right outcome, and the focusin
  // listener drops the free cursor.
  //
  // This listens on the document rather than the screen container because the
  // container is replaced wholesale on every screen refresh, which would take
  // a listener bound to it along with it.
  function installFreeCursorPointer() {
    // Published so the touch layer can tell that a tap on protected text is
    // already answered here. It has its own tap handler for devices where this
    // one is not running, and both firing on one tap is two cursor moves —
    // see installCursorTap in touch.js.
    window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
    window.ThreeSeventyWeb.freeCursorInstalled = true;

    document.addEventListener(
      "pointerdown",
      function (event) {
        // Alt+drag is the block-copy marker; it must not move the cursor.
        if (event.altKey || event.button !== 0) {
          return;
        }
        var grid = window.ThreeSeventyWeb && window.ThreeSeventyWeb.screenGrid;
        if (!grid) {
          return;
        }
        var container = grid.container();
        if (!container || !event.target || typeof event.target.closest !== "function") {
          return;
        }
        if (!container.contains(event.target)) {
          return;
        }
        // A hotspot is a click target in its own right, and a field click
        // belongs to the browser.
        if (event.target.closest(".screen-hotspot") || isScreenInput(event.target)) {
          return;
        }
        var form = grid.form();
        var metrics = grid.metrics();
        if (!form || !metrics) {
          return;
        }
        var cell = grid.pointToCell(metrics, event.clientX, event.clientY);
        var hit = findInputAtCursor(form, cell.row, cell.col);
        if (hit && hit.input) {
          // Clicking the blank tail of a short field: land in the field rather
          // than dropping a free cursor inside one, which would then inhibit
          // typing into the very field that was clicked.
          clearFreeCursor();
          placeCaret(hit.input, hit.caret);
          return;
        }
        setFreeCursor(form, cell.row, cell.col);
        keepKeystrokeSink(form);
      },
      true
    );

    // Deliberately focusing a field — by click or by Tab — puts the cursor back
    // in it, so the free cursor has to go. Without this, clicking straight from
    // protected text into a field would leave the overlay caret behind and
    // typing inhibited.
    document.addEventListener(
      "focusin",
      function (event) {
        if (sinkFocusPending) {
          sinkFocusPending = false;
          return;
        }
        if (freeCursor && isScreenInput(event.target)) {
          clearFreeCursor();
        }
      },
      true
    );
  }

  // keepKeystrokeSink puts DOM focus back on a field after a click on protected
  // text. Clicking a <pre> blurs the active element to <body>, and from there
  // the arrow keys stop being recognised as terminal keys at all — so the
  // cursor would land on the cell the operator clicked and then refuse to move.
  //
  // It runs a frame late on purpose: the blur is the pointerdown's default
  // action, so focusing during the event would just be undone by it.
  function keepKeystrokeSink(form) {
    if (typeof window.requestAnimationFrame !== "function") {
      return;
    }
    window.requestAnimationFrame(function () {
      if (!freeCursor || !form) {
        return;
      }
      var active = document.activeElement;
      if (isScreenInput(active) && form.contains(active)) {
        return;
      }
      var sink = form.querySelector(
        "input[data-x][data-y]:not([disabled]):not([readonly])"
      );
      if (!sink || typeof sink.focus !== "function") {
        return;
      }
      // Suppress the focusin listener above: this focus is bookkeeping, not the
      // operator asking to be in that field.
      sinkFocusPending = true;
      sink.focus();
    });
  }

  // ---------------------------------------------------------------------
  // Local cursor navigation.
  //
  // Tab, Back-Tab, the arrows and Home used to POST to the server, costing a
  // full round-trip (submit -> s3270 action -> screen re-read -> re-render ->
  // DOM replacement) for a movement the host neither sees nor cares about.
  // On a WAN that made routine field-to-field navigation visibly slow. These
  // move the caret in the DOM instead; the resulting position is reported to
  // the host once, with the next AID key, via the cursor_row/cursor_col
  // hidden inputs that setCursorFromTarget already fills in.
  // ---------------------------------------------------------------------

  function placeCaret(el, caret) {
    if (!el || typeof el.focus !== "function") {
      return false;
    }
    el.focus();
    if (typeof el.setSelectionRange !== "function" || typeof el.value !== "string") {
      return true;
    }
    var pos = typeof caret === "number" && isFinite(caret) ? caret : 0;
    if (pos < 0) {
      pos = 0;
    }
    if (pos > el.value.length) {
      pos = el.value.length;
    }
    el.setSelectionRange(pos, pos);
    return true;
  }

  // absoluteCursorOf converts a caret inside a field input into screen
  // coordinates. Continuation lines of a multi-line field start at column 0,
  // matching the offset logic in setCursorFromTarget.
  function absoluteCursorOf(el) {
    var pos = getFieldPosition(el);
    if (!pos) {
      return null;
    }
    var startX = getLineOffsetFromName(el.name || "") > 0 ? 0 : pos.x;
    var caret = typeof el.selectionStart === "number" ? el.selectionStart : 0;
    return { row: pos.y, col: startX + caret };
  }

  function currentScreenInput(form) {
    var active = document.activeElement;
    if (isScreenInput(active) && (!form || form.contains(active))) {
      return active;
    }
    var inputs = getOrderedScreenInputs(form);
    return inputs.length ? inputs[0] : null;
  }

  // moveTab implements 3270 Tab / Back-Tab. Back-Tab first jumps to the start
  // of the current field when the caret is mid-field, which is what the real
  // key does and what muscle memory expects.
  function moveTab(form, back) {
    var inputs = getOrderedScreenInputs(form);
    if (!inputs.length) {
      return false;
    }
    // Tab is defined as field-to-field, so from a protected cell it means the
    // next field after that cell — not the next one after whichever input
    // happens to still hold DOM focus.
    if (freeCursor) {
      var from = { row: freeCursor.row, col: freeCursor.col };
      clearFreeCursor();
      return placeCaret(tabTargetFromCell(inputs, from, back), 0);
    }
    var current = currentScreenInput(form);
    var idx = inputs.indexOf(current);
    if (idx === -1) {
      return placeCaret(inputs[0], 0);
    }
    if (back) {
      var caret = typeof current.selectionStart === "number" ? current.selectionStart : 0;
      if (caret > 0) {
        return placeCaret(current, 0);
      }
      return placeCaret(inputs[(idx - 1 + inputs.length) % inputs.length], 0);
    }
    return placeCaret(inputs[(idx + 1) % inputs.length], 0);
  }

  // tabTargetFromCell picks the field Tab (or Back-Tab) should land on when
  // the cursor starts on a cell rather than in a field. inputs is in reading
  // order, so the answer is the first field after the cell, wrapping round.
  function tabTargetFromCell(inputs, from, back) {
    var i;
    if (back) {
      for (i = inputs.length - 1; i >= 0; i--) {
        var prev = fieldSpan(inputs[i]);
        if (prev && (prev.row < from.row || (prev.row === from.row && prev.start < from.col))) {
          return inputs[i];
        }
      }
      return inputs[inputs.length - 1];
    }
    for (i = 0; i < inputs.length; i++) {
      var span = fieldSpan(inputs[i]);
      if (span && (span.row > from.row || (span.row === from.row && span.start > from.col))) {
        return inputs[i];
      }
    }
    return inputs[0];
  }

  function moveHome(form) {
    var inputs = getOrderedScreenInputs(form);
    if (!inputs.length) {
      return false;
    }
    clearFreeCursor();
    return placeCaret(inputs[0], 0);
  }

  // moveHorizontal walks the cursor one cell left or right, over protected
  // text as readily as through a field, and wrapping round the screen edges.
  //
  // Deviation from a hardware terminal worth knowing about: a field's caret
  // range is bounded by its current text length, not its full width, because
  // the value is stored untrimmed only as far as the host sent it. Arrowing
  // right out of a short value therefore leaves from the end of the field
  // rather than crawling through its blank tail. Typing still fills the field
  // normally.
  function moveHorizontal(form, delta) {
    var dims = screenDims(form);
    if (freeCursor) {
      return moveToCell(form, dims, freeCursor.row, freeCursor.col + delta);
    }
    var current = currentScreenInput(form);
    if (!current) {
      var inputs = getOrderedScreenInputs(form);
      return inputs.length ? placeCaret(inputs[0], 0) : false;
    }
    var caret = typeof current.selectionStart === "number" ? current.selectionStart : 0;
    var next = caret + delta;
    var limit = typeof current.value === "string" ? current.value.length : 0;
    if (next >= 0 && next <= limit) {
      return placeCaret(current, next);
    }
    // Leaving the field. Step from the field's own edge in cells, not from the
    // caret: the cells between the value's end and the field's end belong to
    // this input, and stepping into one of them would clamp straight back to
    // where the cursor already is.
    var span = fieldSpan(current);
    if (!span) {
      return false;
    }
    return moveToCell(form, dims, span.row, delta > 0 ? span.end + 1 : span.start - 1);
  }

  // moveVertical moves exactly one row and keeps the screen column, which is
  // what the key does on a terminal. It used to skip to the next row that had
  // an input on it, because a row of purely protected text was not somewhere
  // the cursor could go; with freeCursor it is.
  function moveVertical(form, delta) {
    var dims = screenDims(form);
    var origin = cursorCell(form);
    if (!origin) {
      var inputs = getOrderedScreenInputs(form);
      return inputs.length ? placeCaret(inputs[0], 0) : false;
    }
    return moveToCell(form, dims, origin.row + delta, origin.col);
  }

  // handleLocalNavigation routes a normalized key name to its local movement.
  // Returns true when the key was a navigation key and has been handled.
  function handleLocalNavigation(key, formId) {
    var form = findForm(formId);
    if (!form) {
      return false;
    }
    // Unformatted screens render as a single textarea with no field grid;
    // let the browser's own caret handling deal with those.
    if (!form.querySelector("input[data-x][data-y]")) {
      return false;
    }
    switch (String(key).trim().toUpperCase()) {
      case "TAB":
        return moveTab(form, false);
      case "BACKTAB":
        return moveTab(form, true);
      case "HOME":
        return moveHome(form);
      case "LEFT":
        return moveHorizontal(form, -1);
      case "RIGHT":
        return moveHorizontal(form, 1);
      case "UP":
        return moveVertical(form, -1);
      case "DOWN":
        return moveVertical(form, 1);
      default:
        return false;
    }
  }

  // ---------------------------------------------------------------------
  // Type-ahead
  // ---------------------------------------------------------------------

  function bufferTypeAhead(entry) {
    if (typeAhead.length >= typeAheadLimit) {
      return false;
    }
    typeAhead.push(entry);
    return true;
  }

  // finishHostRoundTrip replays anything typed while the host held the
  // keyboard, in order. AID keys are deliberately never buffered: replaying a
  // transaction the operator fired blind against a screen they had not yet
  // seen is not a convenience, it is a hazard.
  function finishHostRoundTrip() {
    awaitingHost = false;
    renderOIA();
    if (!typeAhead.length) {
      return;
    }
    var pending = typeAhead;
    typeAhead = [];
    for (var i = 0; i < pending.length; i++) {
      var entry = pending[i];
      if (entry.type === "nav") {
        handleLocalNavigation(entry.key, null);
      } else if (entry.type === "char") {
        typeCharacter(entry.char);
      }
    }
  }

  // typeCharacter applies one printable character to the focused field with
  // full 3270 semantics: numeric-field enforcement, insert vs overtype, and
  // field-overflow detection. Returns false if the terminal refused it.
  function typeCharacter(ch) {
    // The cursor is on protected text. DOM focus is still on a field — that is
    // how the keystroke reached us at all — so without this check the character
    // would land somewhere the operator is not looking.
    if (inhibitOnProtected()) {
      return false;
    }
    var target = document.activeElement;
    if (!isScreenInput(target) || target.disabled || target.readOnly) {
      return false;
    }
    if (target.dataset.numeric === "1" && !numericCharPattern.test(ch)) {
      setOperatorError("Numeric field");
      return false;
    }
    var value = typeof target.value === "string" ? target.value : "";
    var max = target.maxLength > 0 ? target.maxLength : value.length + 1;
    var start = typeof target.selectionStart === "number" ? target.selectionStart : value.length;
    var end = typeof target.selectionEnd === "number" ? target.selectionEnd : start;
    if (start > end) {
      var swap = start;
      start = end;
      end = swap;
    }

    var next;
    if (insertMode) {
      if (value.length >= max) {
        setOperatorError("Field full");
        return false;
      }
      next = value.slice(0, start) + ch + value.slice(end);
    } else {
      // Overtype: consume the character under the caret rather than pushing
      // it right.
      var consumeTo = end > start ? end : Math.min(start + 1, value.length);
      next = value.slice(0, start) + ch + value.slice(consumeTo);
    }
    if (next.length > max) {
      next = next.slice(0, max);
    }
    target.value = next;
    var caret = Math.min(start + 1, next.length);
    if (typeof target.setSelectionRange === "function") {
      target.setSelectionRange(caret, caret);
    }
    target.dispatchEvent(new Event("input", { bubbles: true }));

    // Auto-skip out of a filled field, but only where the application asked
    // for it.
    //
    // On a 3270 auto-skip is the protected+numeric attribute on the field that
    // FOLLOWS this one, and the renderer resolves that into data-autoskip
    // because the browser cannot see a protected field's attributes. This used
    // to approximate the rule as "the field is numeric", which caught the
    // common case — date parts, account digits — and got the rest wrong in
    // both directions: it yanked the caret out of a full numeric field the
    // application wanted you to stay in, and sat still in a full alphanumeric
    // one it wanted you to leave.
    if (target.dataset.autoskip === "1" && next.length >= max && caret >= max) {
      focusNextScreenInput(target, findForm(null));
    }
    return true;
  }

  function focusNextScreenInput(current, form) {
    var inputs = getOrderedScreenInputs(form);
    var idx = inputs.indexOf(current);
    if (idx === -1 || idx >= inputs.length - 1) {
      return false;
    }
    var next = inputs[idx + 1];
    next.focus();
    if (typeof next.setSelectionRange === "function") {
      var len = next.value ? next.value.length : 0;
      next.setSelectionRange(len, len);
    }
    return true;
  }

  function disarmClearConfirmation() {
    clearConfirmArmed = false;
    if (clearConfirmTimer) {
      window.clearTimeout(clearConfirmTimer);
      clearConfirmTimer = 0;
    }
  }

  function armClearConfirmation() {
    clearConfirmArmed = true;
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify("Press Esc again to clear the screen", "warning", {
        duration: clearConfirmWindowMs
      });
    }
    clearConfirmTimer = window.setTimeout(disarmClearConfirmation, clearConfirmWindowMs);
  }

  function handleKeyDownEvent(event, formId) {
    if (!event) {
      return;
    }

    // Never route keyboard events to the terminal while any modal is open.
    if (isModalOpen()) {
      return;
    }

    // Never intercept keystrokes when focus is inside a non-terminal editable
    // element (e.g. the Copilot chat textarea, settings inputs).
    if (isEditableTarget(event.target) && !isScreenInput(event.target)) {
      return;
    }

    // Focus inside the menu bar belongs to the menu bar. isModalOpen() above
    // already covers an OPEN menu; this covers the trigger a keyboard user has
    // just tabbed to, where nothing is open yet and ArrowDown is the key that
    // opens it. Without this the default keymap claims the arrows first — it
    // binds them to cursor movement, which it does wherever focus happens to
    // be — and the menu can only be opened with a mouse.
    if (event.target && typeof event.target.closest === "function" && event.target.closest("[data-app-menu]")) {
      return;
    }

    // Alt+Enter belongs to focus mode, not the host — it is the long-standing
    // full-screen convention for terminal emulators. This handler is on
    // window and so runs before focus-mode.js's document listener, which
    // means declining it has to happen here or the host gets a stray Enter
    // every time someone maximises.
    if (event.altKey && !event.ctrlKey && !event.metaKey && !event.shiftKey && event.key === "Enter") {
      return;
    }

    // A user's own binding wins over the built-in mapping. Checked before
    // everything else so someone who has rebound, say, Ctrl+Enter to PF12
    // gets PF12 rather than the default Enter — the whole point of remapping
    // is to override what the terminal would otherwise do.
    var bound = resolveCustomBinding(event);
    if (bound) {
      event.preventDefault();
      if (isCursorNavigationKey(bound)) {
        if (submitting) {
          bufferTypeAhead({ type: "nav", key: bound });
        } else if (!handleLocalNavigation(bound, formId)) {
          sendAidKey(bound, formId, isScreenInput(event.target) ? event.target : null);
        }
        return;
      }
      if (bound === specialKeys.Insert) {
        setInsertMode(!insertMode);
        return;
      }
      if (bound === specialKeys.Reset && clearOperatorError(formId)) {
        return;
      }
      sendAidKey(bound, formId, event.target);
      return;
    }

    var isEscapeKey = event.key === "Escape" || (event.keyCode || event.which) === 27;
    if (clearConfirmArmed && !isEscapeKey) {
      disarmClearConfirmation();
    }

    var visualKey = mapVisualKey(event);
    if (visualKey) {
      animateVirtualKey(visualKey);
    }

    // Handle Tab key to restrict it to terminal screen inputs only, and only
    // while focus is actually inside the terminal. Without the
    // isInsideTerminalShell guard, Tab would be hijacked everywhere on the
    // page (including toolbar buttons/links), permanently trapping keyboard
    // users inside the terminal (WCAG 2.1.2 — see #terminal-escape-hatch).
    var code = event.keyCode || event.which;
    if (event.key === "Tab" || code === 9) {
      if (!isInsideTerminalShell(event.target)) {
        return;
      }
      // Ctrl+Tab / Ctrl+Shift+Tab is the documented escape hatch (see the
      // visually-hidden hint rendered next to the terminal): deliberately
      // move focus to a dedicated landing target instead of sending a 3270
      // Tab, and suppress the focus lock's next focusin so it doesn't snap
      // focus straight back into the terminal.
      if (event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault();
        var hatch = document.getElementById("terminal-escape-hatch");
        if (hatch) {
          terminalFocusReleasePending = true;
          terminalFocusEscaped = true;
          hatch.focus();
        }
        return;
      }
      var form = findForm(formId);
      if (!event.metaKey && !event.ctrlKey && !event.altKey && !isModalOpen() && form) {
        event.preventDefault();
        var tabKey = event.shiftKey ? "BackTab" : "Tab";
        if (submitting) {
          bufferTypeAhead({ type: "nav", key: tabKey });
        } else if (!handleLocalNavigation(tabKey, formId)) {
          // Unformatted screens render as a single textarea with no field
          // grid, so there is nothing to navigate locally — fall back to
          // letting the host move its own cursor.
          sendAidKey(tabKey, formId, isScreenInput(event.target) ? event.target : null);
        }
      }
      return;
    }

    if (
      !event.metaKey &&
      !event.ctrlKey &&
      !event.altKey &&
      !isModalOpen() &&
      isInsideTerminalShell(event.target) &&
      (event.key === "ArrowUp" ||
        event.key === "ArrowDown" ||
        event.key === "ArrowLeft" ||
        event.key === "ArrowRight" ||
        event.keyCode === 37 ||
        event.keyCode === 38 ||
        event.keyCode === 39 ||
        event.keyCode === 40)
    ) {
      var arrowKey = mapSpecialKey(event);
      if (arrowKey) {
        event.preventDefault();
        if (submitting) {
          bufferTypeAhead({ type: "nav", key: arrowKey });
        } else if (!handleLocalNavigation(arrowKey, formId)) {
          sendAidKey(arrowKey, formId, isScreenInput(event.target) ? event.target : null);
        }
      }
      return;
    }

    // Home is a local cursor movement too, not an AID key.
    if ((event.key === "Home" || code === 36) && !event.ctrlKey && !event.metaKey && !event.altKey) {
      if (isInsideTerminalShell(event.target)) {
        event.preventDefault();
        if (!handleLocalNavigation("HOME", formId)) {
          sendAidKey(specialKeys.Home, formId, isScreenInput(event.target) ? event.target : null);
        }
        return;
      }
    }

    // Insert toggles overtype/insert on the terminal itself; it is not
    // something the host is told about.
    if ((event.key === "Insert" || code === 45) && !event.ctrlKey && !event.metaKey) {
      event.preventDefault();
      setInsertMode(!insertMode);
      return;
    }

    // Escape doubles as Reset while input is inhibited — the fastest way out
    // of an operator error, and otherwise the key does nothing useful here.
    if (isEscapeKey && (operatorError || hostIndicator === "X -f")) {
      event.preventDefault();
      clearOperatorError(formId);
      return;
    }

    // Backspace and Delete are otherwise left to the browser to apply to the
    // focused field. While the cursor is on protected text that field is not
    // where the operator is, so editing it would destroy input they cannot see
    // themselves touching. A terminal inhibits instead.
    if (
      freeCursor &&
      (event.key === "Backspace" || code === 8 || event.key === "Delete" || code === 46)
    ) {
      event.preventDefault();
      inhibitOnProtected();
      return;
    }

    if (isEditableTarget(event.target) && isNativeNavKey(event)) {
      return;
    }

    // Printable characters go through the 3270 field rules (numeric
    // enforcement, insert vs overtype, overflow) rather than straight into
    // the input, and are buffered rather than dropped while the host holds
    // the keyboard.
    if (
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey &&
      !event.isComposing &&
      event.key &&
      event.key.length === 1 &&
      // While the cursor is out on protected text, DOM focus may have ended up
      // on the shell rather than a field. The keystroke still has to reach
      // typeCharacter, which is what raises the operator error.
      (isScreenInput(event.target) || (freeCursor && isInsideTerminalShell(event.target)))
    ) {
      event.preventDefault();
      if (operatorError) {
        return;
      }
      if (submitting) {
        bufferTypeAhead({ type: "char", char: event.key });
        return;
      }
      typeCharacter(event.key);
      return;
    }

    var paKey = mapPaKeys(event);
    if (paKey) {
      event.preventDefault();
      sendAidKey(paKey, formId, event.target);
      return;
    }

    var special = mapSpecialKey(event);
    if (special) {
      event.preventDefault();
      if (special === specialKeys.Reset) {
        if (clearOperatorError(formId)) {
          return;
        }
      }
      if (special === specialKeys.Clear) {
        if (!clearConfirmArmed) {
          armClearConfirmation();
          return;
        }
        disarmClearConfirmation();
      }
      sendAidKey(special, formId, event.target);
      return;
    }

    var pfKey = mapFunctionKey(event);
    if (pfKey) {
      event.preventDefault();
      sendAidKey(pfKey, formId, event.target);
    }
  }

  // sendAidKey gates every host-bound key on the inhibit state. A real
  // terminal refuses AID keys outright while an operator error stands, and
  // never queues one fired during a host wait.
  function sendAidKey(key, formId, target) {
    if (operatorError) {
      renderOIA();
      return;
    }
    if (submitting) {
      return;
    }
    sendFormWithKey(key, formId, target);
  }

  function createButton(key, label, options) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "h3270-key";
    btn.dataset.key = key || "";
    var normalized = options && options.normalizedKey ? options.normalizedKey : normalizeVirtualKey(key || label || "");
    btn.dataset.keyNormalized = normalized;

    var mainLabel = document.createElement("span");
    mainLabel.className = "h3270-key-label";
    mainLabel.textContent = label || key;
    btn.appendChild(mainLabel);

    if (options && options.mapping) {
      var mapping = document.createElement("span");
      mapping.className = "h3270-key-mapping";
      mapping.textContent = options.mapping;
      btn.appendChild(mapping);
    }

    if (options && options.title) {
      btn.title = options.title;
      btn.setAttribute("aria-label", options.title);
    }
    btn.addEventListener("click", function () {
      if (options && Object.prototype.hasOwnProperty.call(options, "inputText")) {
        animateVirtualKey(normalized);
        insertTextIntoFocusedInput(options.inputText);
        return;
      }
      sendFormWithKey(key);
    });
    return btn;
  }

  function normalizeVirtualKey(key) {
    if (!key) {
      return "";
    }
    var upper = String(key).trim().toUpperCase();
    var pfParen = upper.match(/^PF\((\d{1,2})\)$/);
    if (pfParen) {
      return "PF" + pfParen[1];
    }
    var paParen = upper.match(/^PA\((\d)\)$/);
    if (paParen) {
      return "PA" + paParen[1];
    }
    return upper;
  }

  function animateVirtualKey(key) {
    var normalized = normalizeVirtualKey(key);
    if (!normalized) {
      return;
    }
    var selector = '.h3270-key[data-key-normalized="' + normalized + '"]';
    var matches = document.querySelectorAll(selector);
    if (!matches || matches.length === 0) {
      return;
    }
    for (var i = 0; i < matches.length; i++) {
      var btn = matches[i];
      if (btn._activeTimer) {
        clearTimeout(btn._activeTimer);
      }
      btn.classList.add("is-active");
      btn._activeTimer = window.setTimeout(
        (function (el) {
          return function () {
            el.classList.remove("is-active");
          };
        })(btn),
        170
      );
    }
  }

  function getStoredKeypadMode() {
    var mode = "max";
    try {
      var storedMode = window.localStorage.getItem(keypadModeStorageKey);
      if (storedMode === "compact" || storedMode === "full" || storedMode === "max") {
        mode = storedMode;
      } else if (window.localStorage.getItem(keypadCompactStorageKey) === "1") {
        mode = "compact";
      }
    } catch (err) {
      mode = "max";
    }
    return mode;
  }

  function setStoredKeypadMode(mode) {
    try {
      window.localStorage.setItem(keypadModeStorageKey, mode);
      window.localStorage.setItem(keypadCompactStorageKey, mode === "compact" ? "1" : "0");
    } catch (err) {
      // ignore persistence errors
    }
  }

  function notifyTerminalLayoutChange() {
    if (typeof window.sizeScreenContainer === "function") {
      window.sizeScreenContainer();
    }
    try {
      window.dispatchEvent(new CustomEvent("h3270:layout-changed", { detail: { source: "keypad" } }));
    } catch (err) {
      window.dispatchEvent(new Event("h3270:layout-changed"));
    }
  }

  function getKeypadElement() {
    return document.getElementById("keypad");
  }

  function clearKeypadAutoScale(keypad) {
    if (!keypad) {
      return;
    }
    keypad.style.zoom = "";
  }

  function focusModeActive() {
    return document.body.getAttribute("data-focus") === "on";
  }

  // How far the keypad may be scaled down. Focus mode is the only layout with
  // nowhere to scroll, so it is the only one where the keyboard gives up more
  // of itself to keep the terminal whole.
  function keypadMinScale() {
    return focusModeActive() ? 0.3 : 0.45;
  }

  // keypadReservedHeight is how much of the display below the terminal is
  // already spoken for, which is what the terminal's own fitting subtracts
  // before deciding how large it can be. Because the keypad is sized from a
  // share of the viewport in focus mode rather than from the terminal's
  // leftovers, this is a settled number by the time the terminal asks.
  function keypadReservedHeight() {
    var keypad = getKeypadElement();
    if (!keypad || keypad.hidden || keypad.children.length === 0) {
      return 0;
    }
    var height = Math.ceil(keypad.getBoundingClientRect().height || 0);
    return height > 0 ? height + 8 : 0;
  }

  function syncKeypadAutoScale() {
    var keypad = getKeypadElement();
    if (!keypad) {
      return;
    }
    if (keypad.hidden || keypad.children.length === 0 || !keypad.classList.contains("h3270-keypad")) {
      clearKeypadAutoScale(keypad);
      return;
    }

    // Measure the keypad at natural size, then scale it to fit the available
    // viewport width/height beneath the terminal.
    keypad.style.zoom = "";

    var naturalWidth = Math.ceil(keypad.scrollWidth || 0);
    var naturalHeight = Math.ceil(keypad.scrollHeight || 0);
    if (naturalWidth <= 0 || naturalHeight <= 0) {
      return;
    }

    // Remembered so the terminal can be fitted against the room this keypad
    // is entitled to keep. See keypadReservedHeight below.
    keypadNaturalHeight = naturalHeight;

    var keypadRect = keypad.getBoundingClientRect();
    var parent = keypad.parentElement;
    var parentRect = parent ? parent.getBoundingClientRect() : null;
    var availableWidth = Math.max(1, Math.floor((parentRect ? parentRect.width : window.innerWidth) - 4));

    // Outside focus mode the keypad takes the room left beneath the terminal,
    // and anything that does not fit scrolls. In focus mode nothing scrolls,
    // and measuring the leftover room is a feedback loop with the terminal's
    // own fitting: the terminal shrinks, the keypad grows into what it just
    // vacated, and the two chase each other until both overflow the display.
    // So focus mode gives the keyboard a fixed share of the viewport — a
    // number that depends on nothing which is itself being resized — and the
    // terminal takes everything else. That is what "the terminal wins" has to
    // mean to be implementable.
    var availableHeight;
    if (focusModeActive()) {
      availableHeight = Math.max(1, Math.floor(window.innerHeight * FOCUS_KEYPAD_MAX_FRACTION));
    } else {
      availableHeight = Math.max(1, Math.floor(window.innerHeight - keypadRect.top - 8));
    }

    var widthScale = availableWidth / naturalWidth;
    var heightScale = availableHeight / naturalHeight;
    var scale = Math.min(1, widthScale, heightScale);
    if (!isFinite(scale) || scale <= 0) {
      scale = 1;
    }
    // Keep the keyboard usable even on very small viewports. Focus mode is the
    // one place it yields further: the mode exists to give the terminal the
    // display, and a keypad that would not shrink past its comfortable floor
    // is what pushed the terminal's first rows off the top edge — on a 3270
    // panel, the title and message line.
    var floor = keypadMinScale();
    if (scale < floor) {
      scale = floor;
    }

    if (Math.abs(scale - 1) < 0.01) {
      keypad.style.zoom = "";
    } else {
      keypad.style.zoom = scale.toFixed(3);
    }
  }

  function scheduleKeypadAutoScale() {
    if (keypadScaleRafId) {
      return;
    }
    keypadScaleRafId = window.requestAnimationFrame(function () {
      keypadScaleRafId = 0;
      syncKeypadAutoScale();
    });
  }

  function initKeypadAutoScale() {
    var keypad = getKeypadElement();
    if (!keypad) {
      return;
    }

    scheduleKeypadAutoScale();
    window.addEventListener("resize", scheduleKeypadAutoScale);
    window.addEventListener("h3270:layout-changed", scheduleKeypadAutoScale);

    var shell = document.querySelector(".terminal-shell");
    if (typeof ResizeObserver !== "undefined") {
      if (keypadResizeObserver) {
        keypadResizeObserver.disconnect();
      }
      keypadResizeObserver = new ResizeObserver(function () {
        scheduleKeypadAutoScale();
      });
      keypadResizeObserver.observe(keypad);
      if (shell) {
        keypadResizeObserver.observe(shell);
      }
    }

    // Published for the terminal's own fitting, which has to know how much
    // room below it is spoken for before it decides how large it can be.
    window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
    window.ThreeSeventyWeb.keypad = {
      reservedHeight: keypadReservedHeight
    };
  }

  // The sizes the header's one control steps through, in order. "hidden" is
  // the last stop rather than a separate button: putting the keyboard away is
  // the smallest size it has.
  var keypadModeCycle = [
    { id: "compact", label: "Compact", detail: "the keys a screen ends with" },
    { id: "full", label: "Full", detail: "every 3270 command key" },
    { id: "max", label: "MAX", detail: "the whole keyboard" },
    { id: "hidden", label: "Hide", detail: "no keyboard" }
  ];

  function keypadModeIndex(mode) {
    for (var i = 0; i < keypadModeCycle.length; i++) {
      if (keypadModeCycle[i].id === mode) {
        return i;
      }
    }
    return 0;
  }

  function nextKeypadMode(mode) {
    return keypadModeCycle[(keypadModeIndex(mode) + 1) % keypadModeCycle.length].id;
  }

  function applyKeypadMode(container, mode, buttons) {
    container.classList.toggle("is-compact", mode === "compact");
    container.classList.toggle("is-max", mode === "max");
    if (buttons) {
      var current = keypadModeCycle[keypadModeIndex(mode)];
      var next = keypadModeCycle[keypadModeIndex(nextKeypadMode(mode))];
      for (var i = 0; i < buttons.length; i++) {
        var btn = buttons[i];
        btn.dataset.mode = mode;
        btn.textContent = current.label;
        // The label says where the keyboard is; the accessible name and the
        // tooltip say where pressing takes it, because a control that only
        // names its own state gives no clue what it does.
        var hint = "Keyboard size: " + current.label + " — " + current.detail + ". Switch to " + next.label + ".";
        btn.setAttribute("aria-label", hint);
        btn.title = hint;
        if (btn._tippy && typeof btn._tippy.setContent === "function") {
          btn._tippy.setContent(hint);
        }
      }
    }
    scheduleKeypadAutoScale();
    notifyTerminalLayoutChange();
  }

  // The MAX keyboard is laid out as one physical keyboard rather than as rows
  // of buttons: staggered letter block, wide modifiers where the hands expect
  // them, and the cursor cluster and number pad in their own columns to the
  // right. Everything a 3270 keyboard has is on it once — the function row,
  // the PA keys, and the command keys in the positions a terminal keyboard
  // gives them — so MAX has no need of the button groups the smaller modes
  // show, and hides them. Widths are in key units (u), the same currency a
  // real keyboard is cut from: a row is 15u wide and always adds up.

  // A character cap: what it types unshifted, what it types shifted, and how
  // wide it is.
  function charKey(lower, upper, units, label) {
    return { char: [lower, upper], units: units || 1, label: label };
  }

  // An action cap. shiftAction, where given, is what the cap becomes while
  // shift is held — the physical keyboard's answer to having more actions
  // than caps.
  function actionKey(action, label, units, extra) {
    var key = { action: action, label: label, units: units || 1 };
    if (extra) {
      for (var name in extra) {
        if (Object.prototype.hasOwnProperty.call(extra, name)) {
          key[name] = extra[name];
        }
      }
    }
    return key;
  }

  // Regional layouts. Only the parts that actually differ between regions are
  // described here — the rest of the keyboard is the same everywhere and is
  // built around them. A new region is a new entry in this list and nothing
  // else.
  //
  // The ISO layouts (UK and the like) have one more key than ANSI on both the
  // home row and the bottom row, and pay for it with a narrower left shift and
  // an Enter that steps across two rows. That is why the rows are described as
  // data rather than drawn once: the geometry is part of the region, not just
  // the legends.
  var keypadLayouts = [
    {
      id: "us",
      label: "US",
      title: "United States (ANSI)",
      locales: ["en-us", "en-ca", "en-ph", "en"],
      number: [
        charKey("`", "~"), charKey("1", "!"), charKey("2", "@"), charKey("3", "#"),
        charKey("4", "$"), charKey("5", "%"), charKey("6", "^"), charKey("7", "&"),
        charKey("8", "*"), charKey("9", "("), charKey("0", ")"), charKey("-", "_"),
        charKey("=", "+")
      ],
      top: [
        charKey("q", "Q"), charKey("w", "W"), charKey("e", "E"), charKey("r", "R"),
        charKey("t", "T"), charKey("y", "Y"), charKey("u", "U"), charKey("i", "I"),
        charKey("o", "O"), charKey("p", "P"), charKey("[", "{"), charKey("]", "}")
      ],
      topEnd: charKey("\\", "|", 1.5),
      home: [
        charKey("a", "A"), charKey("s", "S"), charKey("d", "D"), charKey("f", "F"),
        charKey("g", "G"), charKey("h", "H"), charKey("j", "J"), charKey("k", "K"),
        charKey("l", "L"), charKey(";", ":"), charKey("'", "\"")
      ],
      homeEnd: actionKey("Enter", "Enter", 2.25),
      shiftLeftUnits: 2.25,
      bottomStart: null,
      bottom: [
        charKey("z", "Z"), charKey("x", "X"), charKey("c", "C"), charKey("v", "V"),
        charKey("b", "B"), charKey("n", "N"), charKey("m", "M"), charKey(",", "<"),
        charKey(".", ">"), charKey("/", "?")
      ]
    },
    {
      id: "uk",
      label: "UK",
      title: "United Kingdom (ISO)",
      locales: ["en-gb", "en-ie", "cy-gb", "gd-gb"],
      number: [
        charKey("`", "¬"), charKey("1", "!"), charKey("2", "\""), charKey("3", "£"),
        charKey("4", "$"), charKey("5", "%"), charKey("6", "^"), charKey("7", "&"),
        charKey("8", "*"), charKey("9", "("), charKey("0", ")"), charKey("-", "_"),
        charKey("=", "+")
      ],
      top: [
        charKey("q", "Q"), charKey("w", "W"), charKey("e", "E"), charKey("r", "R"),
        charKey("t", "T"), charKey("y", "Y"), charKey("u", "U"), charKey("i", "I"),
        charKey("o", "O"), charKey("p", "P"), charKey("[", "{"), charKey("]", "}")
      ],
      // The upper arm of the tall ISO Enter. Both arms send Enter, and sitting
      // flush right one above the other they read as the one stepped key they
      // are on the desk.
      topEnd: actionKey("Enter", "Enter", 1.5),
      home: [
        charKey("a", "A"), charKey("s", "S"), charKey("d", "D"), charKey("f", "F"),
        charKey("g", "G"), charKey("h", "H"), charKey("j", "J"), charKey("k", "K"),
        charKey("l", "L"), charKey(";", ":"), charKey("'", "@"), charKey("#", "~")
      ],
      homeEnd: actionKey("Enter", "⏎", 1.25),
      shiftLeftUnits: 1.25,
      bottomStart: charKey("\\", "|"),
      bottom: [
        charKey("z", "Z"), charKey("x", "X"), charKey("c", "C"), charKey("v", "V"),
        charKey("b", "B"), charKey("n", "N"), charKey("m", "M"), charKey(",", "<"),
        charKey(".", ">"), charKey("/", "?")
      ]
    }
  ];

  function keypadLayoutById(id) {
    for (var i = 0; i < keypadLayouts.length; i++) {
      if (keypadLayouts[i].id === id) {
        return keypadLayouts[i];
      }
    }
    return null;
  }

  // The first layout is the fallback rather than a hardcoded "us", so the list
  // above stays the only place a region is named.
  function defaultKeypadLayout() {
    var languages = [];
    if (window.navigator) {
      if (window.navigator.languages && window.navigator.languages.length) {
        languages = Array.prototype.slice.call(window.navigator.languages);
      } else if (window.navigator.language) {
        languages = [window.navigator.language];
      }
    }
    for (var l = 0; l < languages.length; l++) {
      var tag = String(languages[l] || "").toLowerCase();
      for (var i = 0; i < keypadLayouts.length; i++) {
        var locales = keypadLayouts[i].locales || [];
        for (var m = 0; m < locales.length; m++) {
          if (tag === locales[m]) {
            return keypadLayouts[i].id;
          }
        }
      }
    }
    return keypadLayouts[0].id;
  }

  // Remembered rather than re-detected, because the browser's locale is a
  // guess at which keyboard is on the desk and the operator's correction is
  // not.
  function getStoredKeypadLayout() {
    try {
      var stored = window.localStorage.getItem(keypadLayoutStorageKey);
      if (stored && keypadLayoutById(stored)) {
        return stored;
      }
    } catch (err) {
      /* storage unavailable */
    }
    return defaultKeypadLayout();
  }

  function setStoredKeypadLayout(id) {
    try {
      window.localStorage.setItem(keypadLayoutStorageKey, id);
    } catch (err) {
      /* storage unavailable */
    }
  }

  function createMaxKey(button, units) {
    button.classList.add("h3270-max-key");
    if (units && units !== 1) {
      button.style.setProperty("--u", String(units));
    }
    // A cap that reads "Enter" over a caption reading "Enter" says nothing
    // twice. The caption earns its place only where the physical key differs
    // from the 3270 action printed above it.
    var label = button.querySelector(".h3270-key-label");
    var mapping = button.querySelector(".h3270-key-mapping");
    if (label && mapping && label.textContent.toLowerCase() === mapping.textContent.toLowerCase()) {
      mapping.remove();
    }
    return button;
  }

  // The cap prints the physical key it answers to, and on a 1u cap there is no
  // room for "Shift+F12". The shift glyph says the same thing in one
  // character.
  function pfCapMapping(pfNum) {
    return pfNum > 12 ? "⇧F" + (pfNum - 12) : "F" + pfNum;
  }

  function createMaxSpacer(units) {
    var spacer = document.createElement("span");
    spacer.className = "h3270-max-key h3270-max-spacer";
    spacer.setAttribute("aria-hidden", "true");
    if (units && units !== 1) {
      spacer.style.setProperty("--u", String(units));
    }
    return spacer;
  }

  function makeMaxRow(parent) {
    var row = document.createElement("div");
    row.className = "h3270-max-row";
    parent.appendChild(row);
    return row;
  }

  // The shift state of the on-screen keyboard. One press arms it for the next
  // key and it releases itself, the way a keyboard behaves when the finger
  // comes off the shift; a second press locks it, for anyone paging through
  // PF19 and PF20 or typing a run of capitals. Nothing here touches the
  // physical keyboard's own shift, which the browser handles.
  function createShiftState() {
    var state = "off";
    var listeners = [];
    return {
      active: function () {
        return state !== "off";
      },
      locked: function () {
        return state === "locked";
      },
      // off -> armed -> locked -> off, so one control covers "this key only"
      // and "until I say otherwise" without a second control to explain.
      advance: function () {
        state = state === "off" ? "armed" : (state === "armed" ? "locked" : "off");
        this.notify();
      },
      // Called after a key has used the shift. A lock survives; an arm does
      // not.
      consume: function () {
        if (state === "armed") {
          state = "off";
          this.notify();
        }
      },
      onChange: function (fn) {
        listeners.push(fn);
      },
      notify: function () {
        for (var i = 0; i < listeners.length; i++) {
          listeners[i](state);
        }
      },
      state: function () {
        return state;
      }
    };
  }

  function appendCharCap(row, spec, shift) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "h3270-key";
    var lower = spec.char[0];
    var upper = spec.char[1];
    // The cap is keyed on what it types unshifted, so a physical keypress
    // lights the cap it came from.
    var normalized = "CHAR_" + (lower === " " ? "SPACE" : String(lower).toUpperCase());
    btn.dataset.key = "";
    btn.dataset.keyNormalized = normalized;

    // A letter cap carries one legend, in the case a keyboard prints it in. A
    // symbol cap carries both, shifted legend above, as on the key itself. A
    // cap that types the same either way — the space bar, the number pad —
    // carries its name.
    var isLetterPair = upper === lower.toUpperCase() && lower !== upper && /^[a-z]$/.test(lower);
    if (!isLetterPair && upper !== lower) {
      var shifted = document.createElement("span");
      shifted.className = "h3270-key-mapping h3270-key-shifted";
      shifted.textContent = upper;
      btn.appendChild(shifted);
    }
    var label = document.createElement("span");
    label.className = "h3270-key-label";
    label.textContent = spec.label || (isLetterPair ? upper : lower);
    btn.appendChild(label);

    btn.addEventListener("click", function () {
      var text = shift.active() ? upper : lower;
      animateVirtualKey(normalized);
      insertTextIntoFocusedInput(text);
      shift.consume();
    });
    row.appendChild(createMaxKey(btn, spec.units));
    return btn;
  }

  // An action cap that has a second identity under shift: Tab becomes
  // back-tab, PF1 becomes PF13. The cap re-labels itself so the row always
  // says what it will actually send.
  function appendActionCap(row, spec, shift) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "h3270-key";
    var label = document.createElement("span");
    label.className = "h3270-key-label";
    btn.appendChild(label);
    var mapping = document.createElement("span");
    mapping.className = "h3270-key-mapping";
    btn.appendChild(mapping);

    var current = spec.action;
    function sync() {
      var shifted = shift.active() && spec.shiftAction;
      current = shifted ? spec.shiftAction : spec.action;
      label.textContent = shifted ? (spec.shiftLabel || spec.shiftAction) : spec.label;
      var caption = shifted
        ? (spec.shiftMapping !== undefined ? spec.shiftMapping : defaultKeyFor(spec.shiftAction))
        : (spec.mapping !== undefined ? spec.mapping : defaultKeyFor(spec.action));
      mapping.textContent = caption || "";
      mapping.hidden = !caption || caption.toLowerCase() === label.textContent.toLowerCase();
      btn.dataset.key = current;
      // Kept in step so that pressing the physical key still lights the cap
      // that is currently showing that action.
      btn.dataset.keyNormalized = normalizeVirtualKey(current);
      if (spec.title) {
        btn.title = spec.title(current);
      }
    }

    btn.addEventListener("click", function () {
      sendFormWithKey(current);
      shift.consume();
    });

    if (spec.shiftAction) {
      shift.onChange(sync);
    }
    sync();
    row.appendChild(createMaxKey(btn, spec.units));
    return btn;
  }

  function appendShiftCap(row, units, shift) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "h3270-key h3270-max-shift";
    var label = document.createElement("span");
    label.className = "h3270-key-label";
    label.textContent = "⇧ Shift";
    btn.appendChild(label);
    var caption = document.createElement("span");
    caption.className = "h3270-key-mapping";
    caption.textContent = "PF13-24";
    btn.appendChild(caption);
    btn.addEventListener("click", function () {
      shift.advance();
    });
    shift.onChange(function (state) {
      btn.classList.toggle("is-armed", state === "armed");
      btn.classList.toggle("is-locked", state === "locked");
      btn.setAttribute("aria-pressed", state === "off" ? "false" : "true");
      caption.textContent = state === "locked" ? "Locked" : "PF13-24";
    });
    btn.setAttribute("aria-pressed", "false");
    row.appendChild(createMaxKey(btn, units));
    return btn;
  }

  function appendMaxKeyboardLayout(container, layoutId) {
    var layout = keypadLayoutById(layoutId) || keypadLayouts[0];
    var shift = createShiftState();

    var maxGroup = document.createElement("div");
    maxGroup.className = "h3270-keypad-group h3270-keypad-max";

    var wrapper = document.createElement("div");
    wrapper.className = "h3270-max-layout";

    var main = document.createElement("div");
    main.className = "h3270-max-main";
    // Shows which legend on each cap is live without having to re-write every
    // label the moment shift is pressed.
    shift.onChange(function (state) {
      main.classList.toggle("is-shifted", state !== "off");
    });

    // The function row. Clear takes the Esc position — it is the key Esc is
    // bound to — and the PF caps carry PF13-PF24 under shift, the same trick a
    // physical keyboard plays to get 24 function keys out of 12.
    var fnRow = makeMaxRow(main);
    appendActionCap(fnRow, actionKey("Clear", "Clear", 1.5), shift);
    for (var n = 1; n <= 12; n++) {
      appendActionCap(fnRow, actionKey("PF" + n, "PF" + n, 1, {
        shiftAction: "PF" + (n + 12),
        mapping: pfCapMapping(n),
        shiftMapping: pfCapMapping(n + 12),
        title: function (action) {
          return pfLabelFor(parseInt(action.slice(2), 10));
        }
      }), shift);
    }
    appendActionCap(fnRow, actionKey("Attn", "Attn", 1.5, { mapping: "" }), shift);

    var numberRow = makeMaxRow(main);
    for (var i = 0; i < layout.number.length; i++) {
      appendCharCap(numberRow, layout.number[i], shift);
    }
    appendActionCap(numberRow, actionKey("BackSpace", "Backspace", 2), shift);

    // Tab under shift is back-tab, which is what Shift+Tab does on the
    // physical keyboard and what saves the row a cap of its own.
    var topRow = makeMaxRow(main);
    appendActionCap(topRow, actionKey("Tab", "Tab", 1.5, {
      shiftAction: "BackTab",
      shiftLabel: "BackTab",
      // Printed like the shifted legend on a symbol cap, because that is what
      // it is: the row has no back-tab key of its own, and this is where it
      // went.
      mapping: "⇧BackTab",
      shiftMapping: "Shift+Tab"
    }), shift);
    for (var t = 0; t < layout.top.length; t++) {
      appendCharCap(topRow, layout.top[t], shift);
    }
    appendMaxSpecKey(topRow, layout.topEnd, shift);

    // Reset sits where Caps Lock does. It is the key an operator reaches for
    // without looking, and on a locked keyboard it is the only one that does
    // anything.
    var homeRow = makeMaxRow(main);
    appendActionCap(homeRow, actionKey("Reset", "Reset", 1.75, { mapping: "" }), shift);
    for (var h = 0; h < layout.home.length; h++) {
      appendCharCap(homeRow, layout.home[h], shift);
    }
    appendMaxSpecKey(homeRow, layout.homeEnd, shift);

    var bottomRow = makeMaxRow(main);
    appendShiftCap(bottomRow, layout.shiftLeftUnits, shift);
    if (layout.bottomStart) {
      appendCharCap(bottomRow, layout.bottomStart, shift);
    }
    for (var b = 0; b < layout.bottom.length; b++) {
      appendCharCap(bottomRow, layout.bottom[b], shift);
    }
    appendShiftCap(bottomRow, 2.75, shift);

    // The command keys take the modifier row, where a keyboard puts the keys
    // pressed with a thumb or a little finger rather than aimed at.
    var spaceRow = makeMaxRow(main);
    appendActionCap(spaceRow, actionKey("SysReq", "SysReq", 1.5, { mapping: "" }), shift);
    appendActionCap(spaceRow, actionKey("EraseEOF", "EraseEOF", 1.75, { mapping: "" }), shift);
    appendActionCap(spaceRow, actionKey("EraseInput", "EraseInput", 1.75, { mapping: "" }), shift);
    appendActionCap(spaceRow, actionKey("Dup", "Dup", 1.25, { mapping: "" }), shift);
    appendCharCap(spaceRow, charKey(" ", " ", 5.5, "Space"), shift);
    appendActionCap(spaceRow, actionKey("FieldMark", "FieldMark", 1.5, { mapping: "" }), shift);
    appendActionCap(spaceRow, actionKey("NewLine", "NewLine", 1.75, { mapping: "" }), shift);

    // The cursor cluster: PA keys above the editing keys above the arrow tee,
    // in the block a terminal keyboard keeps between the letters and the
    // number pad.
    var nav = document.createElement("div");
    nav.className = "h3270-max-nav";
    var paRow = makeMaxRow(nav);
    appendActionCap(paRow, actionKey("PA1", "PA1", 1), shift);
    appendActionCap(paRow, actionKey("PA2", "PA2", 1), shift);
    appendActionCap(paRow, actionKey("PA3", "PA3", 1), shift);
    var editRow = makeMaxRow(nav);
    appendActionCap(editRow, actionKey("Insert", "Ins", 1), shift);
    appendActionCap(editRow, actionKey("Delete", "Del", 1), shift);
    appendActionCap(editRow, actionKey("Home", "Home", 1), shift);
    var upRow = makeMaxRow(nav);
    upRow.appendChild(createMaxSpacer(1));
    appendActionCap(upRow, actionKey("Up", "↑", 1), shift);
    upRow.appendChild(createMaxSpacer(1));
    var arrowRow = makeMaxRow(nav);
    appendActionCap(arrowRow, actionKey("Left", "←", 1), shift);
    appendActionCap(arrowRow, actionKey("Down", "↓", 1), shift);
    appendActionCap(arrowRow, actionKey("Right", "→", 1), shift);

    var numpad = document.createElement("div");
    numpad.className = "h3270-max-numpad";
    // The blank row keeps the number pad's 7-8-9 level with the editing keys,
    // the way it sits on a keyboard with a function row above it.
    var padSpacerRow = makeMaxRow(numpad);
    padSpacerRow.appendChild(createMaxSpacer(3));
    var numRows = [
      ["7", "8", "9"],
      ["4", "5", "6"],
      ["1", "2", "3"],
      ["0", ".", "+"]
    ];
    for (var r = 0; r < numRows.length; r++) {
      var nr = makeMaxRow(numpad);
      for (var m = 0; m < numRows[r].length; m++) {
        appendCharCap(nr, charKey(numRows[r][m], numRows[r][m]), shift);
      }
    }

    // The cursor cluster and the number pad ride together in one side block to
    // the right of the letters, where a physical keyboard puts them. Stacking
    // them underneath instead costs seven more rows of height, and height below
    // the terminal is the scarce resource: every row the keyboard takes is a
    // row the 3270 screen does not get.
    var side = document.createElement("div");
    side.className = "h3270-max-side";
    side.appendChild(nav);
    side.appendChild(numpad);

    wrapper.appendChild(main);
    wrapper.appendChild(side);
    maxGroup.appendChild(wrapper);
    container.appendChild(maxGroup);
  }

  // The one cap in each row whose kind depends on the region: a character on
  // ANSI, the upper arm of the stepped Enter on ISO.
  function appendMaxSpecKey(row, spec, shift) {
    if (!spec) {
      return null;
    }
    return spec.char ? appendCharCap(row, spec, shift) : appendActionCap(row, spec, shift);
  }

  // The physical key each 3270 action answers to by default. This is the one
  // description of the built-in mapping: the virtual keypad prints it on the
  // key caps, and the keymap editor shows it beside each action so an
  // operator can see what a key already does before rebinding over it. Kept
  // at module scope rather than inside renderKeypad() because a second copy
  // in the editor would drift the first time a default changed.
  var defaultKeyMappings = {
    Enter: "Enter",
    Tab: "Tab",
    BackTab: "Shift+Tab",
    Clear: "Esc",
    BackSpace: "Backspace",
    Delete: "Delete",
    Insert: "Insert",
    Home: "Home",
    Up: "ArrowUp",
    Down: "ArrowDown",
    Left: "ArrowLeft",
    Right: "ArrowRight",
    PA1: "Alt+F1",
    PA2: "Alt+F2",
    PA3: "Alt+F3"
  };

  function pfMapping(pfNum) {
    if (pfNum >= 1 && pfNum <= 12) {
      return "F" + pfNum;
    }
    return "Shift+F" + (pfNum - 12);
  }

  // What each function key is conventionally for. Only the ones an operator
  // can rely on across applications are named; the rest carry their default
  // physical key instead of a guess at their meaning.
  var pfLabels = {
    PF1: "PF1 Help",
    PF3: "PF3 Exit / Return",
    PF4: "PF4 Return / Exit",
    PF5: "PF5 Refresh / Confirm",
    PF7: "PF7 Page Back / Up",
    PF8: "PF8 Page Forward / Down",
    PF12: "PF12 Cancel / Confirm"
  };

  function pfLabelFor(pfNum) {
    return pfLabels["PF" + pfNum] || pfMapping(pfNum);
  }

  // defaultKeyFor returns the built-in key for an action, or "" when the
  // action has no keyboard default and is reachable only from the keypad,
  // the toolbar or a custom binding.
  function defaultKeyFor(action) {
    var name = String(action || "").trim();
    var pf = name.match(/^PF(\d{1,2})$/);
    if (pf) {
      var n = parseInt(pf[1], 10);
      return n >= 1 && n <= 24 ? pfMapping(n) : "";
    }
    return defaultKeyMappings[name] || "";
  }

  function renderKeypad(containerId) {
    var container = containerId
      ? document.getElementById(containerId)
      : document.getElementById("keypad");
    if (!container) {
      return;
    }

    var mode = getStoredKeypadMode();

    container.innerHTML = "";
    container.classList.add("h3270-keypad");

    var header = document.createElement("div");
    header.className = "h3270-keypad-header";

    var title = document.createElement("strong");
    title.className = "h3270-keypad-title";
    title.textContent = "3270 Virtual Keyboard";
    header.appendChild(title);

    // One control for four states instead of four buttons for four states.
    // The keypad's own header is the last place that can afford a row of
    // chrome, and the sizes are a progression — compact, full, whole
    // keyboard, gone — which is what a single stepping control is for.
    var modeSwitch = document.createElement("div");
    modeSwitch.className = "h3270-keypad-mode-switch";
    var modeToggle = document.createElement("button");
    modeToggle.type = "button";
    modeToggle.className = "h3270-keypad-toggle h3270-keypad-mode-btn";
    modeToggle.addEventListener("click", function () {
      var next = nextKeypadMode(this.dataset.mode || "full");
      if (next === "hidden") {
        // Hiding also winds the size on to the start of the cycle, so the
        // keyboard comes back compact and the next press continues round.
        // Restoring MAX instead would strand compact and full: from MAX the
        // only step is hidden, and hidden would step straight back to MAX.
        var afterHidden = nextKeypadMode("hidden");
        applyKeypadMode(container, afterHidden, [this]);
        setStoredKeypadMode(afterHidden);
        setKeypadVisibility(false);
        return;
      }
      applyKeypadMode(container, next, [this]);
      setStoredKeypadMode(next);
    });
    modeSwitch.appendChild(modeToggle);

    // Which keyboard is on the desk. Guessed once from the browser's locale
    // and remembered after that, because the guess is a guess and the
    // operator's answer is not. Only MAX has letters to arrange, so the
    // picker shows only there.
    var layoutId = getStoredKeypadLayout();
    var layoutPicker = document.createElement("select");
    layoutPicker.className = "h3270-keypad-layout-select";
    layoutPicker.setAttribute("aria-label", "Keyboard layout");
    for (var kl = 0; kl < keypadLayouts.length; kl++) {
      var option = document.createElement("option");
      option.value = keypadLayouts[kl].id;
      option.textContent = keypadLayouts[kl].label;
      option.title = keypadLayouts[kl].title;
      layoutPicker.appendChild(option);
    }
    layoutPicker.value = layoutId;
    layoutPicker.title = "Keyboard layout — " + (keypadLayoutById(layoutId) || keypadLayouts[0]).title;
    layoutPicker.addEventListener("change", function () {
      setStoredKeypadLayout(this.value);
      // The rows differ in shape between regions, not just in legends, so the
      // keyboard is rebuilt rather than relabelled.
      renderKeypad(containerId);
    });
    modeSwitch.insertBefore(layoutPicker, modeToggle);

    header.appendChild(modeSwitch);
    container.appendChild(header);

    var keyMappings = defaultKeyMappings;

    // The button groups below are what compact and full modes show. MAX has
    // the same keys arranged as a keyboard, so it hides these rather than
    // printing every key twice.
    var pfGroup = document.createElement("div");
    pfGroup.className = "h3270-keypad-group h3270-keypad-grouped";

    var pfRowTop = document.createElement("div");
    pfRowTop.className = "h3270-keypad-row h3270-keypad-row--pf";
    for (var i = 1; i <= 12; i++) {
      var pfKeyTop = "PF" + i;
      pfRowTop.appendChild(
        createButton(pfKeyTop, pfKeyTop, {
          title: pfLabels[pfKeyTop] || pfMapping(i),
          mapping: pfMapping(i)
        })
      );
    }
    pfGroup.appendChild(pfRowTop);

    var pfRowBottom = document.createElement("div");
    pfRowBottom.className = "h3270-keypad-row h3270-keypad-row--pf h3270-keypad-extra";
    for (var j = 13; j <= 24; j++) {
      var pfKeyBottom = "PF" + j;
      pfRowBottom.appendChild(
        createButton(pfKeyBottom, pfKeyBottom, {
          title: pfLabels[pfKeyBottom] || pfMapping(j),
          mapping: pfMapping(j)
        })
      );
    }
    pfGroup.appendChild(pfRowBottom);
    container.appendChild(pfGroup);

    var paGroup = document.createElement("div");
    paGroup.className = "h3270-keypad-group h3270-keypad-grouped h3270-keypad-extra";
    var paBlock = document.createElement("div");
    paBlock.className = "h3270-keypad-row";
    paBlock.appendChild(createButton("PA1", "PA1", { mapping: keyMappings.PA1 }));
    paBlock.appendChild(createButton("PA2", "PA2", { mapping: keyMappings.PA2 }));
    paBlock.appendChild(createButton("PA3", "PA3", { mapping: keyMappings.PA3 }));
    paGroup.appendChild(paBlock);
    container.appendChild(paGroup);

    var common = [
      "Enter",
      "Tab",
      "BackTab",
      "Clear",
      "Reset",
      "EraseEOF",
      "EraseInput",
      "Dup",
      "FieldMark",
      "SysReq",
      "Attn",
      "NewLine",
      "BackSpace",
      "Delete",
      "Insert",
      "Home",
      "Up",
      "Down",
      "Left",
      "Right"
    ];
    var commonGroup = document.createElement("div");
    commonGroup.className = "h3270-keypad-group h3270-keypad-grouped";
    var commonBlock = document.createElement("div");
    commonBlock.className = "h3270-keypad-row";
    common.forEach(function (key) {
      var options = {
        mapping: keyMappings[key] || ""
      };
      var btn = createButton(key, key, options);
      if (
        key === "Reset" ||
        key === "EraseEOF" ||
        key === "EraseInput" ||
        key === "Dup" ||
        key === "FieldMark" ||
        key === "SysReq" ||
        key === "Attn" ||
        key === "NewLine" ||
        key === "BackSpace" ||
        key === "Delete" ||
        key === "Insert" ||
        key === "Home"
      ) {
        btn.classList.add("h3270-keypad-extra");
      }
      commonBlock.appendChild(btn);
    });
    commonGroup.appendChild(commonBlock);
    container.appendChild(commonGroup);

    appendMaxKeyboardLayout(container, layoutId);
    applyKeypadMode(container, mode, [modeToggle]);
    scheduleKeypadAutoScale();
  }

  function syncKeypadToggleUi(visible) {
    var toggle = document.querySelector("[data-keypad-toggle]");
    if (!toggle) {
      return;
    }
    var label = visible ? "Hide virtual keyboard" : "Show virtual keyboard";
    toggle.setAttribute("aria-label", label);
    toggle.setAttribute("title", label);
    toggle.setAttribute("data-tippy-content", label);
    toggle.setAttribute("aria-pressed", visible ? "true" : "false");
    toggle.classList.toggle("is-active", visible);
    if (toggle._tippy && typeof toggle._tippy.setContent === "function") {
      toggle._tippy.setContent(label);
    }
  }

  function setKeypadVisibility(nextVisible) {
    var keypad = document.getElementById("keypad");
    if (!keypad) {
      return;
    }

    var previousHidden = keypad.hidden;
    keypad.hidden = !nextVisible;
    syncKeypadToggleUi(nextVisible);

    if (nextVisible && keypad.children.length === 0) {
      renderKeypad();
    }

    scheduleKeypadAutoScale();
    notifyTerminalLayoutChange();

    var body = "keypad=" + encodeURIComponent(nextVisible ? "on" : "off");
    fetch("/prefs/keypad", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8" },
      body: body
    }).then(function (response) {
      if (!response.ok) {
        throw new Error("failed");
      }
    }).catch(function () {
      keypad.hidden = previousHidden;
      syncKeypadToggleUi(!keypad.hidden);
      scheduleKeypadAutoScale();
      notifyTerminalLayoutChange();
    });
  }

  function initKeypadVisibilityToggle() {
    var toggle = document.querySelector("[data-keypad-toggle]");
    var keypad = document.getElementById("keypad");
    if (!toggle || !keypad) {
      return;
    }

    syncKeypadToggleUi(!keypad.hidden);

    toggle.addEventListener("click", function () {
      var nextVisible = keypad.hidden;
      setKeypadVisibility(nextVisible);
    });
  }

  // The virtual keypad's entry point. Navigation keys resolve locally here
  // too, so clicking Tab on the on-screen keypad is as instant as pressing
  // it, and Reset clears a client-side operator error without a round-trip.
  window.sendKey = function (key, formId) {
    var normalized = String(key || "").trim().toUpperCase();
    if (normalized === "INSERT") {
      setInsertMode(!insertMode);
      return;
    }
    if (normalized === "RESET" && clearOperatorError(formId)) {
      return;
    }
    if (isCursorNavigationKey(key)) {
      animateVirtualKey(key);
      if (handleLocalNavigation(key, formId)) {
        return;
      }
      // No field grid to navigate (an unformatted screen); let the host do it.
    }
    sendAidKey(key, formId, document.activeElement);
  };

  window.installKeyHandler = function (formId) {
    if (!keydownInstalled) {
      window.addEventListener(
        "keydown",
        function (event) {
          handleKeyDownEvent(event, formId);
        },
        true
      );
      keydownInstalled = true;
      installFreeCursorPointer();
      // The terminal is resizable and the cell size changes with it, so the
      // overlay caret has to be re-placed rather than left at stale pixels.
      window.addEventListener("resize", function () {
        if (freeCursor) {
          paintFreeCursor();
        }
      });
    }
    var form = findForm(formId);
    if (form) {
      if (!form.dataset.keyHandlerInstalled) {
        form.addEventListener("submit", function () {
          var keyInput = form.querySelector('input[name="key"]');
          if (!keyInput) {
            return;
          }
          if (!submitting || !keyInput.value) {
            keyInput.value = specialKeys.Enter;
          }
        });
        // Auto-skip and overtype used to be driven from generic input/focusin
        // listeners, which meant they also fired for pastes and for focus
        // changes the operator did not type. typeCharacter now owns both, so
        // they apply to real keystrokes only.

        // Pasting into a numeric field strips what the field cannot hold
        // rather than refusing the paste outright. Copying an account number
        // that arrived with spaces or dashes in it is completely routine, and
        // rejecting the whole paste for one stray character would be a worse
        // answer than quietly landing the digits.
        form.addEventListener("paste", function (event) {
          var target = event.target;
          if (!isScreenInput(target) || target.dataset.numeric !== "1") {
            return;
          }
          var clipboard = event.clipboardData || window.clipboardData;
          if (!clipboard) {
            return;
          }
          var text = clipboard.getData("text") || "";
          var cleaned = text.replace(/[^0-9.,\-+]/g, "");
          if (cleaned === text) {
            return;
          }
          event.preventDefault();
          if (cleaned) {
            insertTextIntoFocusedInput(cleaned);
          }
          if (!cleaned) {
            setOperatorError("Numeric field");
          }
        });

        form.dataset.keyHandlerInstalled = "1";
      }
    }
  };

  window.renderKeypad = function (containerId) {
    renderKeypad(containerId);
  };

  // Exposed so screen-live.js (the idle SSE push consumer) can patch the
  // OIA status line and restore focus/caret the same way a manual submit's
  // async response does, including honoring terminalFocusEscaped so a
  // server-pushed update can't drag focus back into the terminal after the
  // user deliberately left it via the Ctrl+Tab escape hatch.
  window.updateScreenStatusLine = updateScreenStatusLine;
  window.restoreScreenFocus = restoreScreenFocus;

  // Refresh the 3270 screen HTML from the server without a form submit.
  // Called by Copilot tools after state-changing actions so the terminal
  // display stays in sync.
  window.refreshScreenContent = function () {
    return fetch("/screen/content", {
      headers: { Accept: "application/json", "Cache-Control": "no-cache" },
      credentials: "same-origin"
    })
      .then(function (response) {
        if (!response.ok) return;
        return response.json();
      })
      .then(function (payload) {
        if (!payload || typeof payload.html !== "string") return;
        var container = document.querySelector(".screen-container");
        if (!container) return;
        container.innerHTML = payload.html;
        var updatedForm = container.querySelector("form.renderer-form");
        var updatedFormId = updatedForm ? (updatedForm.id || updatedForm.getAttribute("name")) : null;
        if (typeof window.installKeyHandler === "function") window.installKeyHandler(updatedFormId);
        if (typeof window.sizeScreenContainer === "function") window.sizeScreenContainer();
      })
      .catch(function (err) {
        console.error("Failed to refresh screen content:", err);
      });
  };

  // Terminal state other modules need: the command palette drives Reset and
  // insert mode, and screen-copy.js needs to know whether a field is focused.
  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.terminal = {
    toggleInsertMode: function () {
      setInsertMode(!insertMode);
    },
    isInsertMode: function () {
      return insertMode;
    },
    reset: function (formId) {
      if (!clearOperatorError(formId)) {
        sendAidKey(specialKeys.Reset, formId, document.activeElement);
      }
    },
    sendKey: function (key, formId) {
      window.sendKey(key, formId);
    },
    focusTerminal: focusTerminalContext,
    defaultKeyFor: defaultKeyFor
  };

  document.addEventListener("DOMContentLoaded", function () {
    renderKeypad();
    initKeypadVisibilityToggle();
    initKeypadAutoScale();
    installTerminalFocusLock();
    renderOIA();

    var sizeSlider = document.querySelector("[data-terminal-size-slider]");
    if (sizeSlider) {
      sizeSlider.addEventListener("input", scheduleKeypadAutoScale);
      sizeSlider.addEventListener("change", function () {
        scheduleKeypadAutoScale();
        window.requestAnimationFrame(function () {
          focusTerminalContext();
        });
      });
    }
  });
})();
