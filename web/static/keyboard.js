(function () {
  "use strict";

  var submitting = false;
  var keydownInstalled = false;

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

  function sendFormWithKey(key, formId) {
    if (submitting) {
      return;
    }
    var form = findForm(formId);
    if (!form) {
      return;
    }
    var keyInput = form.querySelector('input[name="key"]');
    if (!keyInput) {
      return;
    }
    keyInput.value = key;
    submitting = true;
    form.submit();
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

  function handleKeyDownEvent(event, formId) {
    if (!event) {
      return;
    }

    // Handle Tab key to restrict it to terminal screen inputs only
    var code = event.keyCode || event.which;
    if (event.key === "Tab" || code === 9) {
      var form = findForm(formId);
      if (form && event.target && (event.target.form === form || form.contains(event.target))) {
        // Allow Tab navigation within the terminal screen form
        event.preventDefault();
        sendFormWithKey(event.shiftKey ? "BackTab" : "Tab", formId);
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
          sendFormWithKey(arrowKey, formId);
        }
        return;
      }
      return;
    }

    var paKey = mapPaKeys(event);
    if (paKey) {
      event.preventDefault();
      sendFormWithKey(paKey, formId);
      return;
    }

    var special = mapSpecialKey(event);
    if (special) {
      event.preventDefault();
      sendFormWithKey(special, formId);
      return;
    }

    var pfKey = mapFunctionKey(event);
    if (pfKey) {
      event.preventDefault();
      sendFormWithKey(pfKey, formId);
    }
  }

  function createButton(key, label) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "h3270-key";
    btn.dataset.key = key;
    btn.textContent = label || key;
    btn.addEventListener("click", function () {
      sendFormWithKey(key);
    });
    return btn;
  }

  function renderKeypad(containerId) {
    var container = containerId
      ? document.getElementById(containerId)
      : document.getElementById("keypad");
    if (!container) {
      return;
    }

    container.innerHTML = "";

    var pfBlock = document.createElement("div");
    pfBlock.className = "h3270-keypad-row";
    for (var i = 1; i <= 24; i++) {
      pfBlock.appendChild(createButton("PF" + i, "PF" + i));
    }
    container.appendChild(pfBlock);

    var paBlock = document.createElement("div");
    paBlock.className = "h3270-keypad-row";
    paBlock.appendChild(createButton("PA1", "PA1"));
    paBlock.appendChild(createButton("PA2", "PA2"));
    paBlock.appendChild(createButton("PA3", "PA3"));
    container.appendChild(paBlock);

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
    var commonBlock = document.createElement("div");
    commonBlock.className = "h3270-keypad-row";
    common.forEach(function (key) {
      commonBlock.appendChild(createButton(key, key));
    });
    container.appendChild(commonBlock);
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
  });
})();
