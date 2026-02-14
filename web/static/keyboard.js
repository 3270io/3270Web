(function () {
  "use strict";

  var submitting = false;
  var keydownInstalled = false;
  var keySubmitDelayMs = 65;
  var keypadCompactStorageKey = "h3270KeypadCompact";
  var keypadModeStorageKey = "h3270KeypadMode";
  var lastKnownCursorRow = null;
  var lastKnownCursorCol = null;
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
    var inputStartX = pos.x + 1;
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
        return;
      }
      request.then(
        function () {
          submitting = false;
        },
        function () {
          submitting = false;
        }
      );
    }, keySubmitDelayMs);
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

  function isModalOpen() {
    var selectors = [
      "[data-settings-modal]",
      "[data-logs-modal]",
      "[data-disconnect-modal]",
      "[data-about-modal]",
      "[data-modal]"
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

  function installTerminalFocusLock() {
    document.addEventListener(
      "pointerdown",
      function (event) {
        if (!shouldKeepTerminalFocus(event.target)) {
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
          focusTerminalInput();
        });
      },
      true
    );

    document.addEventListener(
      "focusin",
      function (event) {
        if (isModalOpen()) {
          return;
        }
        if (isInsideTerminalShell(event.target)) {
          return;
        }
        if (event.target.closest("[data-terminal-size-slider]")) {
          return;
        }
        if (event.target.matches && event.target.matches('input[type="range"]')) {
          return;
        }
        window.requestAnimationFrame(function () {
          focusTerminalInput();
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

  function handleAutoAdvance(event, form) {
    if (!event || event.isComposing) {
      return;
    }
    var target = event.target;
    if (!form || !isScreenInput(target) || target.disabled || target.readOnly) {
      return;
    }
    var max = target.maxLength;
    if (!max || max < 1) {
      return;
    }
    var value = target.value || "";
    if (value.length < max) {
      return;
    }
    if (typeof target.selectionStart === "number" && typeof target.selectionEnd === "number") {
      if (target.selectionStart !== target.selectionEnd || target.selectionEnd !== value.length) {
        return;
      }
    }
    focusNextScreenInput(target, form);
  }

  function handleTypeoverOnFocus(event) {
    var target = event.target;
    if (!isScreenInput(target) || target.disabled || target.readOnly) {
      return;
    }
    var max = target.maxLength;
    var value = target.value || "";
    if (!max || max < 1 || value.length < max) {
      return;
    }
    if (typeof target.setSelectionRange === "function") {
      target.setSelectionRange(0, value.length);
    }
  }

  function handleTypeoverKey(event) {
    if (!event || event.isComposing) {
      return;
    }
    if (event.metaKey || event.ctrlKey || event.altKey) {
      return;
    }
    if (!event.key || event.key.length !== 1) {
      return;
    }
    var target = event.target;
    if (!isScreenInput(target) || target.disabled || target.readOnly) {
      return;
    }
    if (typeof target.selectionStart !== "number" || typeof target.selectionEnd !== "number") {
      return;
    }
    if (target.selectionStart !== target.selectionEnd) {
      return;
    }
    var pos = target.selectionStart;
    var value = target.value || "";
    if (pos >= value.length) {
      return;
    }
    if (typeof target.setSelectionRange === "function") {
      target.setSelectionRange(pos, pos + 1);
    }
  }

  function handleKeyDownEvent(event, formId) {
    if (!event) {
      return;
    }
    var visualKey = mapVisualKey(event);
    if (visualKey) {
      animateVirtualKey(visualKey);
    }

    // Handle Tab key to restrict it to terminal screen inputs only
    var code = event.keyCode || event.which;
    if (event.key === "Tab" || code === 9) {
      var form = findForm(formId);
      if (form && event.target && (event.target.form === form || form.contains(event.target))) {
        // Allow Tab navigation within the terminal screen form
        event.preventDefault();
          sendFormWithKey(event.shiftKey ? "BackTab" : "Tab", formId, event.target);
      }
      // Allow normal browser Tab navigation outside the terminal screen
      return;
    }

    if (isEditableTarget(event.target) && isNativeNavKey(event)) {
      if (
        event.key === "ArrowUp" ||
        event.key === "ArrowDown" ||
        event.key === "ArrowLeft" ||
        event.key === "ArrowRight" ||
        event.keyCode === 37 ||
        event.keyCode === 38 ||
        event.keyCode === 39 ||
        event.keyCode === 40
      ) {
        var arrowKey = mapSpecialKey(event);
        if (arrowKey) {
          event.preventDefault();
          sendFormWithKey(arrowKey, formId, event.target);
        }
        return;
      }
      return;
    }

    handleTypeoverKey(event);

    var paKey = mapPaKeys(event);
    if (paKey) {
      event.preventDefault();
      sendFormWithKey(paKey, formId, event.target);
      return;
    }

    var special = mapSpecialKey(event);
    if (special) {
      event.preventDefault();
      sendFormWithKey(special, formId, event.target);
      return;
    }

    var pfKey = mapFunctionKey(event);
    if (pfKey) {
      event.preventDefault();
      sendFormWithKey(pfKey, formId, event.target);
    }
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
      PF3: "PF3 Exit",
      PF4: "PF4 Return",
      PF5: "PF5 Refresh",
      PF7: "PF7 Up",
      PF8: "PF8 Down",
      PF12: "PF12 Cancel"
    };

    var keyMappings = {
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

  window.sendKey = function (key, formId) {
    sendFormWithKey(key, formId);
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
        form.addEventListener("input", function (event) {
          handleAutoAdvance(event, form);
        });
        form.addEventListener("focusin", handleTypeoverOnFocus);
        form.dataset.keyHandlerInstalled = "1";
      }
    }
  };

  window.renderKeypad = function (containerId) {
    renderKeypad(containerId);
  };

  document.addEventListener("DOMContentLoaded", function () {
    renderKeypad();
    initKeypadVisibilityToggle();
    installTerminalFocusLock();

    var sizeSlider = document.querySelector("[data-terminal-size-slider]");
    if (sizeSlider) {
      sizeSlider.addEventListener("change", function () {
        window.requestAnimationFrame(function () {
          focusTerminalInput();
        });
      });
    }
  });
})();
