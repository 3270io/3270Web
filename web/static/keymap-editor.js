// Keyboard mapping editor.
//
// Rebinding is done by capture, not by picking a key from a dropdown: the
// operator presses the key they want and the dialog records it. A list of
// every key name a browser can report would be long, full of names nobody
// recognises, and still wrong about their keyboard layout.
(function () {
  "use strict";

  var modal = null;
  var listEl = null;
  var statusEl = null;
  var filterEl = null;
  var capturing = null; // the action currently awaiting a keypress

  function keymap() {
    return window.ThreeSeventyWeb && window.ThreeSeventyWeb.keymap;
  }

  function setStatus(message, isError) {
    if (!statusEl) {
      return;
    }
    statusEl.textContent = message || "";
    statusEl.classList.toggle("is-error", !!isError);
  }

  // The built-in key for an action, shown greyed beside it so someone can
  // see what a key already does before rebinding over it, rather than
  // discovering the collision afterwards. keyboard.js owns the table — this
  // only asks it, so the two can never disagree.
  function defaultKey(action) {
    var terminal = window.ThreeSeventyWeb && window.ThreeSeventyWeb.terminal;
    if (!terminal || typeof terminal.defaultKeyFor !== "function") {
      return "";
    }
    return terminal.defaultKeyFor(action);
  }

  function render() {
    if (!listEl) {
      return;
    }
    var filter = (filterEl ? filterEl.value : "").trim().toLowerCase();
    listEl.innerHTML = "";

    var groups = keymap().actionGroups;
    for (var g = 0; g < groups.length; g++) {
      var group = groups[g];
      var matching = group.actions.filter(function (action) {
        if (!filter) {
          return true;
        }
        var custom = keymap().signaturesFor(action).join(" ").toLowerCase();
        return (
          action.toLowerCase().indexOf(filter) !== -1 ||
          defaultKey(action).toLowerCase().indexOf(filter) !== -1 ||
          custom.indexOf(filter) !== -1
        );
      });
      if (!matching.length) {
        continue;
      }

      var heading = document.createElement("h4");
      heading.className = "keymap-group";
      heading.textContent = group.name;
      listEl.appendChild(heading);

      for (var i = 0; i < matching.length; i++) {
        listEl.appendChild(renderRow(matching[i]));
      }
    }
  }

  function renderRow(action) {
    var row = document.createElement("div");
    row.className = "keymap-row";
    if (capturing === action) {
      row.classList.add("is-capturing");
    }

    var name = document.createElement("span");
    name.className = "keymap-action";
    name.textContent = action;
    row.appendChild(name);

    var custom = keymap().signaturesFor(action);
    var keys = document.createElement("span");
    keys.className = "keymap-keys";
    if (capturing === action) {
      keys.textContent = "Press a key…";
      keys.classList.add("is-waiting");
    } else if (custom.length) {
      keys.textContent = custom.join(", ");
    } else {
      // Some actions (Attn, SysReq, Dup, FieldMark…) deliberately have no
      // keyboard default. Saying "keypad only" rather than showing a dash
      // answers the question the dash raises — how do I reach it today?
      var fallback = defaultKey(action);
      keys.textContent = fallback || "Keypad only";
      keys.classList.add("is-default");
      if (!fallback) {
        keys.classList.add("is-unbound");
      }
    }
    row.appendChild(keys);

    var assign = document.createElement("button");
    assign.type = "button";
    assign.className = "keymap-action-btn";
    assign.textContent = capturing === action ? "Cancel" : "Rebind";
    assign.setAttribute("aria-label", (capturing === action ? "Cancel rebinding " : "Rebind ") + action);
    assign.addEventListener("click", function () {
      capturing = capturing === action ? null : action;
      setStatus(
        capturing
          ? "Press the key combination for " + action + ". Esc cancels."
          : ""
      );
      render();
    });
    row.appendChild(assign);

    var reset = document.createElement("button");
    reset.type = "button";
    reset.className = "keymap-action-btn";
    reset.textContent = "Default";
    reset.disabled = custom.length === 0;
    reset.setAttribute("aria-label", "Restore the default key for " + action);
    reset.addEventListener("click", function () {
      for (var i = 0; i < custom.length; i++) {
        keymap().unbind(custom[i]);
      }
      setStatus(action + " is back to its default key.");
      render();
    });
    row.appendChild(reset);

    return row;
  }

  // Capture runs at the document, in the capture phase, so the terminal's own
  // handler never sees the keystroke being recorded.
  function onCaptureKeydown(event) {
    if (!capturing || !isOpen()) {
      return;
    }
    event.preventDefault();
    // Immediate, not just stopPropagation: the command palette and the
    // terminal's own key handler are peers on this target, and a key being
    // recorded must not also be acted on.
    event.stopImmediatePropagation();

    if (event.key === "Escape") {
      capturing = null;
      setStatus("Rebinding cancelled.");
      render();
      return;
    }

    var signature = keymap().signatureFor(event);
    if (!signature) {
      // A bare modifier — keep waiting for the real key.
      return;
    }

    var existing = keymap().all()[signature];
    if (existing && existing !== capturing) {
      // Binding is a move, not a copy: one physical key cannot mean two
      // things, and saying so beats silently stealing it.
      setStatus(signature + " was bound to " + existing + " — moved to " + capturing + ".");
    } else {
      setStatus(signature + " now sends " + capturing + ".");
    }
    keymap().bind(signature, capturing);
    capturing = null;
    render();
  }

  function exportMap() {
    var data = JSON.stringify(keymap().all(), null, 2);
    var blob = new Blob([data], { type: "application/json" });
    var url = URL.createObjectURL(blob);
    var link = document.createElement("a");
    link.href = url;
    link.download = "3270web-keymap.json";
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    window.setTimeout(function () {
      URL.revokeObjectURL(url);
    }, 10000);
    setStatus("Exported " + keymap().count() + " custom binding(s).");
  }

  function importFile(file, kind) {
    var reader = new FileReader();
    reader.onload = function () {
      var text = String(reader.result || "");
      if (kind === "pcomm") {
        var result = keymap().importPcomm(text);
        var count = keymap().applyImported(result.bindings, false);
        render();
        if (!count) {
          setStatus("Nothing in that file could be mapped to a 3270 action.", true);
          return;
        }
        // Report the shortfall rather than letting an operator discover it by
        // pressing keys that do nothing.
        var message = "Imported " + count + " binding(s).";
        if (result.skipped.length) {
          message += " " + result.skipped.length + " line(s) were not recognised and were skipped.";
        }
        setStatus(message, result.skipped.length > 0);
        return;
      }
      try {
        var parsed = JSON.parse(text);
        var added = keymap().applyImported(parsed, false);
        render();
        setStatus("Imported " + added + " binding(s).");
      } catch (_) {
        setStatus("That file is not a valid 3270Web keymap.", true);
      }
    };
    reader.onerror = function () {
      setStatus("Could not read that file.", true);
    };
    reader.readAsText(file);
  }

  function open() {
    if (!modal) {
      return;
    }
    // keymap.js carries the whole model; without it the dialog would render
    // an empty list and silently discard every rebind, so say so instead.
    if (!keymap()) {
      if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
        window.ThreeSeventyWeb.notify("Keyboard mapping is unavailable — keymap.js did not load.", "error");
      }
      return;
    }
    modal.hidden = false;
    capturing = null;
    setStatus("");
    render();
    if (filterEl) {
      filterEl.focus();
    }
  }

  function close() {
    if (!modal) {
      return;
    }
    modal.hidden = true;
    capturing = null;
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  function build() {
    modal = document.createElement("div");
    modal.className = "keymap-modal";
    modal.hidden = true;
    modal.setAttribute("data-keymap-modal", "");
    modal.innerHTML = [
      '<div class="keymap-modal-backdrop" data-keymap-close></div>',
      '<div class="keymap-modal-content" role="dialog" aria-modal="true" aria-labelledby="keymap-title">',
      '  <div class="keymap-modal-header">',
      '    <h3 id="keymap-title">Keyboard mapping</h3>',
      '    <button type="button" data-keymap-close>Close</button>',
      "  </div>",
      '  <input type="search" class="keymap-filter" data-keymap-filter placeholder="Filter actions or keys…" aria-label="Filter actions">',
      '  <div class="keymap-list" data-keymap-list></div>',
      '  <div class="keymap-actions">',
      '    <span class="keymap-status" data-keymap-status aria-live="polite"></span>',
      '    <button type="button" data-keymap-import-json>Import JSON</button>',
      '    <button type="button" data-keymap-import-pcomm>Import PCOMM</button>',
      '    <button type="button" data-keymap-export>Export</button>',
      '    <button type="button" class="danger" data-keymap-reset>Reset all</button>',
      "  </div>",
      '  <p class="subtle keymap-note">Custom bindings layer over the built-in keys, which keep working unless you rebind them. Combinations the browser reserves for itself (Ctrl+W, Ctrl+T, F11) cannot be captured by any web page. Bindings are stored in this browser — use Export to move them.</p>',
      '  <input type="file" accept="application/json,.json" data-keymap-file-json hidden>',
      '  <input type="file" accept=".kmp,.KMP,text/plain" data-keymap-file-pcomm hidden>',
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    listEl = modal.querySelector("[data-keymap-list]");
    statusEl = modal.querySelector("[data-keymap-status]");
    filterEl = modal.querySelector("[data-keymap-filter]");

    filterEl.addEventListener("input", render);
    modal.querySelector("[data-keymap-export]").addEventListener("click", exportMap);

    var jsonFile = modal.querySelector("[data-keymap-file-json]");
    var pcommFile = modal.querySelector("[data-keymap-file-pcomm]");
    modal.querySelector("[data-keymap-import-json]").addEventListener("click", function () {
      jsonFile.click();
    });
    modal.querySelector("[data-keymap-import-pcomm]").addEventListener("click", function () {
      pcommFile.click();
    });
    jsonFile.addEventListener("change", function () {
      if (jsonFile.files && jsonFile.files[0]) {
        importFile(jsonFile.files[0], "json");
        jsonFile.value = "";
      }
    });
    pcommFile.addEventListener("change", function () {
      if (pcommFile.files && pcommFile.files[0]) {
        importFile(pcommFile.files[0], "pcomm");
        pcommFile.value = "";
      }
    });

    modal.querySelector("[data-keymap-reset]").addEventListener("click", function () {
      if (!keymap().count()) {
        setStatus("There are no custom bindings to clear.");
        return;
      }
      if (!window.confirm("Remove all custom key bindings and go back to the defaults?")) {
        return;
      }
      keymap().clear();
      render();
      setStatus("All custom bindings removed.");
    });

    var closers = modal.querySelectorAll("[data-keymap-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }
  }

  // These listen on `window` in the capture phase, and are registered at
  // module scope rather than inside build().
  //
  // Both details are load-bearing. The command palette also captures on
  // `window` (Ctrl+K) and calls stopImmediatePropagation, and window capture
  // runs before any document listener — so a document-level capture here
  // could never record Ctrl+K, and that combination would be permanently
  // unbindable. Registering on the same target at module scope, from a file
  // loaded ahead of ui-polish.js, puts this handler first instead.
  //
  // Combinations the browser itself reserves (Ctrl+W, Ctrl+T, F11) still
  // cannot be captured by any web page; the dialog says so rather than
  // letting an operator wonder why the key never took.
  window.addEventListener("keydown", onCaptureKeydown, true);
  window.addEventListener(
    "keydown",
    function (event) {
      if (isOpen() && !capturing && event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
        close();
      }
    },
    true
  );

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }
    build();
    var trigger = document.querySelector("[data-keymap-open]");
    if (trigger) {
      trigger.addEventListener("click", open);
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.keymapEditor = { open: open, close: close, isOpen: isOpen };
})();
