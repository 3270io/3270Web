// Custom keyboard mapping.
//
// The physical-key to 3270-action mapping was hard-coded, so a shop whose
// operators have twenty years of muscle memory from PCOMM or Quick3270 had to
// retrain them. Every desktop emulator ships a remapping dialog; Quick3270
// additionally imports PCOMM keyboard files, which is what makes a migration
// a morning rather than a project.
//
// A binding is a normalised key signature ("Ctrl+Shift+F3") mapped to a 3270
// action name ("PF15"). User bindings are layered over the built-in defaults
// rather than replacing them, so remapping one key does not silently unbind
// the other forty.
//
// Storage is per browser, not server-side: unlike a connection profile, which
// is deployment configuration an administrator sets once, a keyboard layout
// is a personal preference and there is no user identity here to attach it
// to. Export/import covers sharing and backup.
//
// Must load before keyboard.js, which asks it to resolve every keystroke.
(function () {
  "use strict";

  var STORAGE_KEY = "3270Web.keymap.v1";
  var bindings = {};

  // The 3270 actions a key can be bound to, grouped for the editor. These are
  // exactly the names cmd/3270Web/keys.go accepts, so a binding cannot name
  // an action the server will reject.
  var ACTION_GROUPS = [
    {
      name: "Attention",
      actions: ["Enter", "Clear", "Reset", "Attn", "SysReq"]
    },
    {
      name: "Editing",
      actions: ["EraseEOF", "EraseInput", "Delete", "BackSpace", "Dup", "FieldMark", "NewLine", "Insert"]
    },
    {
      name: "Cursor",
      actions: ["Tab", "BackTab", "Home", "Up", "Down", "Left", "Right"]
    },
    {
      name: "Program function",
      actions: (function () {
        var out = [];
        for (var i = 1; i <= 24; i++) {
          out.push("PF" + i);
        }
        return out;
      })()
    },
    {
      name: "Program attention",
      actions: ["PA1", "PA2", "PA3"]
    }
  ];

  var ALL_ACTIONS = (function () {
    var out = [];
    for (var i = 0; i < ACTION_GROUPS.length; i++) {
      out = out.concat(ACTION_GROUPS[i].actions);
    }
    return out;
  })();

  function isKnownAction(action) {
    return ALL_ACTIONS.indexOf(action) !== -1;
  }

  // signatureFor turns a keydown into a stable, human-readable key name.
  // Modifiers are always in the same order so "Shift+Ctrl+F1" and
  // "Ctrl+Shift+F1" cannot become two different bindings.
  function signatureFor(event) {
    if (!event || !event.key) {
      return "";
    }
    var key = event.key;
    // A bare modifier is not a binding.
    if (key === "Control" || key === "Shift" || key === "Alt" || key === "Meta") {
      return "";
    }
    var parts = [];
    if (event.ctrlKey) parts.push("Ctrl");
    if (event.altKey) parts.push("Alt");
    if (event.shiftKey) parts.push("Shift");
    if (event.metaKey) parts.push("Meta");

    // Printable characters are recorded upper-case so Shift+a and Shift+A do
    // not become separate bindings.
    if (key.length === 1) {
      key = key.toUpperCase();
    }
    parts.push(key);
    return parts.join("+");
  }

  function load() {
    try {
      var raw = localStorage.getItem(STORAGE_KEY);
      if (!raw) {
        bindings = {};
        return bindings;
      }
      var parsed = JSON.parse(raw);
      bindings = sanitise(parsed);
    } catch (_) {
      bindings = {};
    }
    return bindings;
  }

  // sanitise drops anything that is not a signature mapped to a known action.
  // A keymap can arrive from an imported file, so it is untrusted input.
  function sanitise(raw) {
    var out = {};
    if (!raw || typeof raw !== "object") {
      return out;
    }
    var keys = Object.keys(raw);
    for (var i = 0; i < keys.length; i++) {
      var signature = String(keys[i]).trim();
      var action = String(raw[keys[i]] || "").trim();
      if (!signature || !isKnownAction(action)) {
        continue;
      }
      out[signature] = action;
    }
    return out;
  }

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(bindings));
    } catch (_) { /* private browsing; the mapping still applies this session */ }
  }

  function all() {
    return Object.assign({}, bindings);
  }

  function bind(signature, action) {
    signature = String(signature || "").trim();
    if (!signature || !isKnownAction(action)) {
      return false;
    }
    // A physical key can only mean one thing, so binding it moves it.
    bindings[signature] = action;
    persist();
    return true;
  }

  function unbind(signature) {
    if (Object.prototype.hasOwnProperty.call(bindings, signature)) {
      delete bindings[signature];
      persist();
      return true;
    }
    return false;
  }

  function clearAll() {
    bindings = {};
    persist();
  }

  // resolve returns the action a keydown is bound to, or "" to let the
  // built-in mapping handle it.
  function resolve(event) {
    var signature = signatureFor(event);
    if (!signature) {
      return "";
    }
    return bindings[signature] || "";
  }

  // signaturesFor lists the custom keys bound to an action, so the editor can
  // show what an action currently answers to.
  function signaturesFor(action) {
    var out = [];
    var keys = Object.keys(bindings);
    for (var i = 0; i < keys.length; i++) {
      if (bindings[keys[i]] === action) {
        out.push(keys[i]);
      }
    }
    out.sort();
    return out;
  }

  /* ------------------------------------------------------------------ */
  /* PCOMM keyboard file import                                          */
  /* ------------------------------------------------------------------ */

  // PCOMM .KMP files are INI-shaped, with entries that name a key position
  // and the function bound to it. The dialect varies between PCOMM versions
  // and this parser is deliberately tolerant and deliberately honest: it maps
  // what it recognises and REPORTS what it did not, rather than silently
  // dropping lines and leaving an operator to discover the gaps by pressing
  // keys. A partial import that tells you what is missing beats a
  // confident-looking one that lies.
  var PCOMM_FUNCTIONS = {
    enter: "Enter",
    clear: "Clear",
    reset: "Reset",
    attn: "Attn",
    sysreq: "SysReq",
    eraseeof: "EraseEOF",
    erase_eof: "EraseEOF",
    eraseinput: "EraseInput",
    erase_input: "EraseInput",
    dup: "Dup",
    fieldmark: "FieldMark",
    field_mark: "FieldMark",
    newline: "NewLine",
    tab: "Tab",
    backtab: "BackTab",
    home: "Home",
    cursorup: "Up",
    cursordown: "Down",
    cursorleft: "Left",
    cursorright: "Right",
    up: "Up",
    down: "Down",
    left: "Left",
    right: "Right",
    backspace: "BackSpace",
    delete: "Delete",
    insert: "Insert"
  };

  function pcommFunctionToAction(raw) {
    var value = String(raw || "").trim().toLowerCase();
    value = value.replace(/^\[|\]$/g, "").replace(/[\s-]/g, "");
    if (!value) {
      return "";
    }
    var pf = value.match(/^pf(\d{1,2})$/);
    if (pf) {
      var n = parseInt(pf[1], 10);
      return n >= 1 && n <= 24 ? "PF" + n : "";
    }
    var pa = value.match(/^pa(\d)$/);
    if (pa) {
      var m = parseInt(pa[1], 10);
      return m >= 1 && m <= 3 ? "PA" + m : "";
    }
    return PCOMM_FUNCTIONS[value] || "";
  }

  // pcommKeyToSignature converts PCOMM's key naming to ours. PCOMM writes
  // things like "F3", "SHIFT+F3", "ALT+F1", "CTRL+A".
  function pcommKeyToSignature(raw) {
    var value = String(raw || "").trim();
    if (!value) {
      return "";
    }
    var tokens = value.split(/[+ ]+/).filter(Boolean);
    var mods = { Ctrl: false, Alt: false, Shift: false };
    var base = "";
    for (var i = 0; i < tokens.length; i++) {
      var t = tokens[i].toLowerCase();
      if (t === "ctrl" || t === "control") {
        mods.Ctrl = true;
      } else if (t === "alt") {
        mods.Alt = true;
      } else if (t === "shift" || t === "shft") {
        mods.Shift = true;
      } else {
        base = tokens[i];
      }
    }
    if (!base) {
      return "";
    }
    var fn = base.match(/^[fF](\d{1,2})$/);
    if (fn) {
      base = "F" + parseInt(fn[1], 10);
    } else if (base.length === 1) {
      base = base.toUpperCase();
    } else {
      // Normalise the few named keys we understand; anything else is
      // reported as unmapped rather than guessed at.
      var named = {
        enter: "Enter",
        tab: "Tab",
        escape: "Escape",
        esc: "Escape",
        home: "Home",
        insert: "Insert",
        ins: "Insert",
        delete: "Delete",
        del: "Delete",
        backspace: "Backspace",
        up: "ArrowUp",
        down: "ArrowDown",
        left: "ArrowLeft",
        right: "ArrowRight"
      }[base.toLowerCase()];
      if (!named) {
        return "";
      }
      base = named;
    }
    var parts = [];
    if (mods.Ctrl) parts.push("Ctrl");
    if (mods.Alt) parts.push("Alt");
    if (mods.Shift) parts.push("Shift");
    parts.push(base);
    return parts.join("+");
  }

  // importPcomm parses a PCOMM keyboard file and returns what it understood
  // along with the lines it could not, so the caller can show both.
  function importPcomm(text) {
    var imported = {};
    var skipped = [];
    var lines = String(text || "").split(/\r?\n/);
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line.charAt(0) === ";" || line.charAt(0) === "#" || line.charAt(0) === "[") {
        continue;
      }
      var eq = line.indexOf("=");
      if (eq === -1) {
        continue;
      }
      var left = line.slice(0, eq).trim();
      var right = line.slice(eq + 1).trim();
      // PCOMM often writes several fields separated by semicolons; the first
      // is the function.
      right = right.split(";")[0].trim();
      // Entries are commonly "KEY<name>" or just the key name.
      left = left.replace(/^key[_ ]?/i, "");

      var signature = pcommKeyToSignature(left);
      var action = pcommFunctionToAction(right);
      if (!signature || !action) {
        skipped.push(line);
        continue;
      }
      imported[signature] = action;
    }
    return { bindings: imported, skipped: skipped };
  }

  function applyImported(imported, replace) {
    var clean = sanitise(imported);
    if (replace) {
      bindings = clean;
    } else {
      bindings = Object.assign({}, bindings, clean);
    }
    persist();
    return Object.keys(clean).length;
  }

  load();

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.keymap = {
    actionGroups: ACTION_GROUPS,
    allActions: ALL_ACTIONS,
    isKnownAction: isKnownAction,
    signatureFor: signatureFor,
    resolve: resolve,
    all: all,
    signaturesFor: signaturesFor,
    bind: bind,
    unbind: unbind,
    clear: clearAll,
    reload: load,
    importPcomm: importPcomm,
    applyImported: applyImported,
    count: function () {
      return Object.keys(bindings).length;
    }
  };
})();
