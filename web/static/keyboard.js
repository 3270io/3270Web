(function () {
  "use strict";

  var submitting = false;

  var specialKeys = {
    Enter: "Enter",
    BackSpace: "BackSpace",
    Delete: "Delete",
    Insert: "Insert",
    Home: "Home",
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
      form = document.querySelector("form.h3270-form");
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

  function handleKeyDownEvent(event, formId) {
    if (!event) {
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
    window.addEventListener(
      "keydown",
      function (event) {
        handleKeyDownEvent(event, formId);
      },
      true
    );
  };

  window.renderKeypad = function (containerId) {
    renderKeypad(containerId);
  };

  document.addEventListener("DOMContentLoaded", function () {
    renderKeypad();
  });
})();
