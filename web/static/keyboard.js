(function () {
  "use strict";

  var submitting = false;
  var keydownInstalled = false;
  var keySubmitDelayMs = 65;
  var keypadCompactStorageKey = "h3270KeypadCompact";
  var keypadModeStorageKey = "h3270KeypadMode";
  var keypadScaleRafId = 0;
  var keypadResizeObserver = null;
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
    if (target && !isCursorNavigationKey(key)) {
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
      "[data-keymap-modal]"
    ];
    for (var i = 0; i < selectors.length; i++) {
      var el = document.querySelector(selectors[i]);
      if (el && !el.hidden) {
        return true;
      }
    }
    return false;
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
    if (target.closest("[data-terminal-controls], [data-terminal-tools-toggle]")) {
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
    // Allow terminal tools interactions (size buttons, fit, reset, widget toggle)
    // to run normally; focus is restored on click handler.
    if (target.closest("[data-terminal-controls], [data-terminal-tools-toggle]")) {
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
        if (event.target.closest("[data-terminal-controls], [data-terminal-tools-toggle]")) {
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

  function inputRowOf(el) {
    var pos = getFieldPosition(el);
    return pos ? pos.y : null;
  }

  function inputStartColOf(el) {
    var pos = getFieldPosition(el);
    if (!pos) {
      return 0;
    }
    return getLineOffsetFromName(el.name || "") > 0 ? 0 : pos.x;
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

  function moveHome(form) {
    var inputs = getOrderedScreenInputs(form);
    if (!inputs.length) {
      return false;
    }
    return placeCaret(inputs[0], 0);
  }

  // moveHorizontal walks the caret one cell left or right, crossing into the
  // adjacent field at a field boundary and wrapping around the screen.
  //
  // Deviation from a hardware terminal worth knowing about: a field's caret
  // range is bounded by its current text length, not its full width, because
  // the value is stored untrimmed only as far as the host sent it. Arrowing
  // right therefore skips the blank tail of a short value and lands on the
  // next field. Typing still fills the field normally.
  function moveHorizontal(form, delta) {
    var inputs = getOrderedScreenInputs(form);
    if (!inputs.length) {
      return false;
    }
    var current = currentScreenInput(form);
    var idx = inputs.indexOf(current);
    if (idx === -1) {
      return placeCaret(inputs[0], 0);
    }
    var caret = typeof current.selectionStart === "number" ? current.selectionStart : 0;
    var next = caret + delta;
    var limit = typeof current.value === "string" ? current.value.length : 0;
    if (next >= 0 && next <= limit) {
      return placeCaret(current, next);
    }
    var neighbourIdx = (idx + (delta > 0 ? 1 : -1) + inputs.length) % inputs.length;
    var neighbour = inputs[neighbourIdx];
    var neighbourLen = typeof neighbour.value === "string" ? neighbour.value.length : 0;
    return placeCaret(neighbour, delta > 0 ? 0 : neighbourLen);
  }

  // moveVertical keeps the caret in the same screen column where it can. The
  // DOM only holds unprotected fields, so rows made entirely of protected
  // text are skipped rather than landed on — a real terminal would park the
  // cursor there, but there is nowhere in this rendering to put it.
  function moveVertical(form, delta) {
    var inputs = getOrderedScreenInputs(form);
    if (!inputs.length) {
      return false;
    }
    var current = currentScreenInput(form);
    var origin = current ? absoluteCursorOf(current) : null;
    if (!origin) {
      return placeCaret(inputs[0], 0);
    }

    var rows = [];
    for (var i = 0; i < inputs.length; i++) {
      var row = inputRowOf(inputs[i]);
      if (row !== null && rows.indexOf(row) === -1) {
        rows.push(row);
      }
    }
    rows.sort(function (a, b) {
      return a - b;
    });

    // Rows strictly in the direction of travel, nearest first, then wrapping
    // round to the far end of the screen.
    var ordered = [];
    var j;
    if (delta > 0) {
      for (j = 0; j < rows.length; j++) {
        if (rows[j] > origin.row) {
          ordered.push(rows[j]);
        }
      }
      for (j = 0; j < rows.length; j++) {
        if (rows[j] <= origin.row) {
          ordered.push(rows[j]);
        }
      }
    } else {
      for (j = rows.length - 1; j >= 0; j--) {
        if (rows[j] < origin.row) {
          ordered.push(rows[j]);
        }
      }
      for (j = rows.length - 1; j >= 0; j--) {
        if (rows[j] >= origin.row) {
          ordered.push(rows[j]);
        }
      }
    }

    for (var k = 0; k < ordered.length; k++) {
      var targetRow = ordered[k];
      var hit = findInputAtCursor(form, targetRow, origin.col);
      if (hit && hit.input) {
        return placeCaret(hit.input, hit.caret);
      }
      var nearest = nearestInputOnRow(inputs, targetRow, origin.col);
      if (nearest) {
        return placeCaret(nearest, origin.col - inputStartColOf(nearest));
      }
    }
    return false;
  }

  function nearestInputOnRow(inputs, row, col) {
    var best = null;
    var bestDistance = null;
    for (var i = 0; i < inputs.length; i++) {
      if (inputRowOf(inputs[i]) !== row) {
        continue;
      }
      var start = inputStartColOf(inputs[i]);
      var distance = Math.abs(start - col);
      if (bestDistance === null || distance < bestDistance) {
        bestDistance = distance;
        best = inputs[i];
      }
    }
    return best;
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
      isScreenInput(event.target)
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

    var keypadRect = keypad.getBoundingClientRect();
    var parent = keypad.parentElement;
    var parentRect = parent ? parent.getBoundingClientRect() : null;
    var availableWidth = Math.max(1, Math.floor((parentRect ? parentRect.width : window.innerWidth) - 4));
    var availableHeight = Math.max(1, Math.floor(window.innerHeight - keypadRect.top - 8));

    var widthScale = availableWidth / naturalWidth;
    var heightScale = availableHeight / naturalHeight;
    var scale = Math.min(1, widthScale, heightScale);
    if (!isFinite(scale) || scale <= 0) {
      scale = 1;
    }
    // Keep the keyboard usable even on very small viewports.
    if (scale < 0.45) {
      scale = 0.45;
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
  }

  function applyKeypadMode(container, mode, buttons) {
    container.classList.toggle("is-compact", mode === "compact");
    container.classList.toggle("is-max", mode === "max");
    if (buttons) {
      for (var i = 0; i < buttons.length; i++) {
        var btn = buttons[i];
        var active = btn.dataset.mode === mode;
        btn.classList.toggle("is-active", active);
        btn.setAttribute("aria-pressed", active ? "true" : "false");
      }
    }
    scheduleKeypadAutoScale();
    notifyTerminalLayoutChange();
  }

  function createTextKey(label, text, options) {
    var normalized = "CHAR_" + (text === " " ? "SPACE" : String(text).toUpperCase());
    var opts = options || {};
    opts.inputText = text;
    opts.normalizedKey = normalized;
    return createButton("", label, opts);
  }

  function appendMaxKeyboardLayout(container) {
    var maxGroup = document.createElement("div");
    maxGroup.className = "h3270-keypad-group h3270-keypad-max";

    var layout = document.createElement("div");
    layout.className = "h3270-max-layout";

    var main = document.createElement("div");
    main.className = "h3270-max-main";

    var rows = [
      [{ l: "`", t: "`" }, { l: "1", t: "1" }, { l: "2", t: "2" }, { l: "3", t: "3" }, { l: "4", t: "4" }, { l: "5", t: "5" }, { l: "6", t: "6" }, { l: "7", t: "7" }, { l: "8", t: "8" }, { l: "9", t: "9" }, { l: "0", t: "0" }, { l: "-", t: "-" }, { l: "=", t: "=" }],
      [{ l: "Q", t: "q" }, { l: "W", t: "w" }, { l: "E", t: "e" }, { l: "R", t: "r" }, { l: "T", t: "t" }, { l: "Y", t: "y" }, { l: "U", t: "u" }, { l: "I", t: "i" }, { l: "O", t: "o" }, { l: "P", t: "p" }, { l: "[", t: "[" }, { l: "]", t: "]" }, { l: "\\", t: "\\" }],
      [{ l: "A", t: "a" }, { l: "S", t: "s" }, { l: "D", t: "d" }, { l: "F", t: "f" }, { l: "G", t: "g" }, { l: "H", t: "h" }, { l: "J", t: "j" }, { l: "K", t: "k" }, { l: "L", t: "l" }, { l: ";", t: ";" }, { l: "'", t: "'" }],
      [{ l: "Z", t: "z" }, { l: "X", t: "x" }, { l: "C", t: "c" }, { l: "V", t: "v" }, { l: "B", t: "b" }, { l: "N", t: "n" }, { l: "M", t: "m" }, { l: ",", t: "," }, { l: ".", t: "." }, { l: "/", t: "/" }]
    ];

    for (var r = 0; r < rows.length; r++) {
      var row = document.createElement("div");
      row.className = "h3270-max-row";
      for (var c = 0; c < rows[r].length; c++) {
        row.appendChild(createTextKey(rows[r][c].l, rows[r][c].t));
      }
      main.appendChild(row);
    }

    var bottom = document.createElement("div");
    bottom.className = "h3270-max-row";
    var backspace = createButton("BackSpace", "Backspace", { mapping: "Backspace" });
    backspace.classList.add("h3270-max-wide");
    bottom.appendChild(backspace);
    var tab = createButton("Tab", "Tab", { mapping: "Tab" });
    tab.classList.add("h3270-max-medium");
    bottom.appendChild(tab);
    var space = createTextKey("Space", " ", { mapping: "Space" });
    space.classList.add("h3270-max-space");
    bottom.appendChild(space);
    var enter = createButton("Enter", "Enter", { mapping: "Enter" });
    enter.classList.add("h3270-max-medium");
    bottom.appendChild(enter);
    main.appendChild(bottom);

    var nav = document.createElement("div");
    nav.className = "h3270-max-nav";
    var navTop = document.createElement("div");
    navTop.className = "h3270-max-row";
    navTop.appendChild(createButton("Insert", "Ins", { mapping: "Insert" }));
    navTop.appendChild(createButton("Delete", "Del", { mapping: "Delete" }));
    navTop.appendChild(createButton("Home", "Home", { mapping: "Home" }));
    nav.appendChild(navTop);
    var arrows = document.createElement("div");
    arrows.className = "h3270-max-arrows";
    arrows.appendChild(createButton("Up", "↑", { mapping: "ArrowUp" }));
    var middle = document.createElement("div");
    middle.className = "h3270-max-row";
    middle.appendChild(createButton("Left", "←", { mapping: "ArrowLeft" }));
    middle.appendChild(createButton("Down", "↓", { mapping: "ArrowDown" }));
    middle.appendChild(createButton("Right", "→", { mapping: "ArrowRight" }));
    arrows.appendChild(middle);
    nav.appendChild(arrows);

    var numpad = document.createElement("div");
    numpad.className = "h3270-max-numpad";
    var numRows = [
      ["7", "8", "9"],
      ["4", "5", "6"],
      ["1", "2", "3"],
      ["0", ".", "+"]
    ];
    for (var n = 0; n < numRows.length; n++) {
      var nr = document.createElement("div");
      nr.className = "h3270-max-row";
      for (var m = 0; m < numRows[n].length; m++) {
        nr.appendChild(createTextKey(numRows[n][m], numRows[n][m], { mapping: "Numpad" }));
      }
      numpad.appendChild(nr);
    }

    layout.appendChild(main);
    layout.appendChild(nav);
    layout.appendChild(numpad);
    maxGroup.appendChild(layout);
    container.appendChild(maxGroup);
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

    var modeSwitch = document.createElement("div");
    modeSwitch.className = "h3270-keypad-mode-switch";
    var modes = [
      { id: "compact", label: "Compact" },
      { id: "full", label: "Full" },
      { id: "max", label: "MAX" }
    ];
    var modeButtons = [];
    for (var mb = 0; mb < modes.length; mb++) {
      var modeButton = document.createElement("button");
      modeButton.type = "button";
      modeButton.className = "h3270-keypad-toggle h3270-keypad-mode-btn";
      modeButton.dataset.mode = modes[mb].id;
      modeButton.textContent = modes[mb].label;
      modeButton.addEventListener("click", function () {
        var nextMode = this.dataset.mode || "full";
        applyKeypadMode(container, nextMode, modeButtons);
        setStoredKeypadMode(nextMode);
      });
      modeButtons.push(modeButton);
      modeSwitch.appendChild(modeButton);
    }

    var hideButton = document.createElement("button");
    hideButton.type = "button";
    hideButton.className = "h3270-keypad-toggle h3270-keypad-mode-btn h3270-keypad-hide-btn";
    hideButton.textContent = "Hide";
    hideButton.setAttribute("aria-label", "Hide virtual keyboard");
    hideButton.addEventListener("click", function () {
      setKeypadVisibility(false);
    });
    modeSwitch.appendChild(hideButton);

    header.appendChild(modeSwitch);
    container.appendChild(header);

    var pfLabels = {
      PF1: "PF1 Help",
      PF3: "PF3 Exit / Return",
      PF4: "PF4 Return / Exit",
      PF5: "PF5 Refresh / Confirm",
      PF7: "PF7 Page Back / Up",
      PF8: "PF8 Page Forward / Down",
      PF12: "PF12 Cancel / Confirm"
    };

    var keyMappings = defaultKeyMappings;

    var pfGroup = document.createElement("div");
    pfGroup.className = "h3270-keypad-group";

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
    paGroup.className = "h3270-keypad-group h3270-keypad-extra";
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
    commonGroup.className = "h3270-keypad-group";
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

    appendMaxKeyboardLayout(container);
    applyKeypadMode(container, mode, modeButtons);
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
