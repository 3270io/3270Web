// Find on screen.
//
// Ctrl+F in the browser searches the DOM, which for this terminal means it
// misses every input value — the numbers an operator is actually looking for
// live in <input value="..."> attributes, not in text nodes. It also cannot
// match across the field boundaries the renderer splices into the <pre>, so
// searching for a name that happens to straddle one silently fails.
//
// This searches the character grid instead, which is what is on the display.
(function () {
  "use strict";

  var matches = [];
  var active = -1;
  var overlays = [];
  var bar = null;
  var input = null;
  var status = null;

  function grid() {
    return window.ThreeSeventyWeb.screenGrid;
  }

  function clearOverlays() {
    for (var i = 0; i < overlays.length; i++) {
      if (overlays[i].parentNode) {
        overlays[i].parentNode.removeChild(overlays[i]);
      }
    }
    overlays = [];
  }

  function search(term) {
    matches = [];
    active = -1;
    if (!term) {
      return;
    }
    var g = grid();
    var built = g.build();
    if (!built) {
      return;
    }
    var needle = term.toLowerCase();
    for (var row = 0; row < built.rows; row++) {
      var line = built.cells[row].join("").toLowerCase();
      var from = 0;
      while (true) {
        var at = line.indexOf(needle, from);
        if (at === -1) {
          break;
        }
        matches.push({ row: row, from: at, to: at + needle.length - 1 });
        // Overlapping matches are found too ("aa" in "aaa" is two hits),
        // which is what a user scanning a screen expects.
        from = at + 1;
      }
    }
    if (matches.length) {
      active = 0;
    }
  }

  function paint() {
    clearOverlays();
    var g = grid();
    var layer = g.overlayLayer();
    var metrics = g.metrics();
    if (!layer || !metrics) {
      return;
    }
    for (var i = 0; i < matches.length; i++) {
      var m = matches[i];
      var el = document.createElement("div");
      el.className = "screen-find-match" + (i === active ? " is-active" : "");
      el.setAttribute("aria-hidden", "true");
      g.positionOverCells(el, metrics, {
        top: m.row,
        bottom: m.row,
        left: m.from,
        right: m.to
      });
      layer.appendChild(el);
      overlays.push(el);
    }
  }

  function setStatus() {
    if (!status) {
      return;
    }
    var term = input ? input.value : "";
    if (!term) {
      status.textContent = "";
      return;
    }
    if (!matches.length) {
      status.textContent = "No matches";
      return;
    }
    status.textContent = active + 1 + " of " + matches.length;
  }

  function run() {
    search(input ? input.value : "");
    paint();
    setStatus();
  }

  function step(delta) {
    if (!matches.length) {
      return;
    }
    active = (active + delta + matches.length) % matches.length;
    paint();
    setStatus();
    // Put the 3270 cursor on the match. Someone who searched for a field is
    // usually about to type in it, and this saves them finding it by hand.
    // The move is local, so it costs nothing.
    moveCursorToMatch(matches[active]);
  }

  function moveCursorToMatch(match) {
    var form = grid().form();
    if (!form || !match) {
      return;
    }
    var inputs = form.querySelectorAll("input[data-x][data-y]");
    for (var i = 0; i < inputs.length; i++) {
      var el = inputs[i];
      var x = parseInt(el.dataset.x, 10);
      var y = parseInt(el.dataset.y, 10);
      var w = parseInt(el.dataset.w, 10);
      if (isNaN(x) || isNaN(y) || y !== match.row) {
        continue;
      }
      if (isNaN(w) || w <= 0) {
        w = el.maxLength > 0 ? el.maxLength : 0;
      }
      if (match.from < x || match.from >= x + w) {
        continue;
      }
      el.focus();
      var caret = match.from - x;
      if (typeof el.setSelectionRange === "function" && typeof el.value === "string") {
        var pos = Math.min(caret, el.value.length);
        el.setSelectionRange(pos, pos);
      }
      return;
    }
  }

  function open() {
    if (!bar) {
      return;
    }
    bar.hidden = false;
    if (input) {
      input.focus();
      input.select();
    }
    run();
  }

  function close() {
    if (!bar) {
      return;
    }
    bar.hidden = true;
    matches = [];
    active = -1;
    clearOverlays();
    setStatus();
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.terminal) {
      window.ThreeSeventyWeb.terminal.focusTerminal();
    }
  }

  function isOpen() {
    return !!bar && !bar.hidden;
  }

  function build() {
    bar = document.createElement("div");
    bar.className = "screen-find-bar";
    bar.hidden = true;
    bar.setAttribute("role", "search");
    bar.innerHTML =
      '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">' +
      '<path d="M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0A4.5 4.5 0 1 1 14 9.5 4.5 4.5 0 0 1 9.5 14z"/></svg>' +
      '<input type="text" class="screen-find-input" data-find-input autocomplete="off" ' +
      'spellcheck="false" aria-label="Find on screen" placeholder="Find on screen…">' +
      '<span class="screen-find-status" data-find-status aria-live="polite"></span>' +
      '<button type="button" class="screen-find-btn" data-find-prev aria-label="Previous match">&#8593;</button>' +
      '<button type="button" class="screen-find-btn" data-find-next aria-label="Next match">&#8595;</button>' +
      '<button type="button" class="screen-find-btn" data-find-close aria-label="Close find">&times;</button>';

    var shell = document.querySelector(".terminal-shell");
    if (shell && shell.parentNode) {
      shell.parentNode.insertBefore(bar, shell);
    } else {
      document.body.appendChild(bar);
    }

    input = bar.querySelector("[data-find-input]");
    status = bar.querySelector("[data-find-status]");

    input.addEventListener("input", run);
    input.addEventListener("keydown", function (event) {
      // The find field is a normal text box; the terminal must not treat what
      // is typed here as host input.
      event.stopPropagation();
      if (event.key === "Enter") {
        event.preventDefault();
        step(event.shiftKey ? -1 : 1);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        close();
      }
    });

    bar.querySelector("[data-find-next]").addEventListener("click", function () {
      step(1);
    });
    bar.querySelector("[data-find-prev]").addEventListener("click", function () {
      step(-1);
    });
    bar.querySelector("[data-find-close]").addEventListener("click", close);
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".screen-container")) {
      return;
    }
    build();

    var trigger = document.querySelector("[data-find-open]");
    if (trigger) {
      trigger.addEventListener("click", function () {
        if (isOpen()) {
          close();
        } else {
          open();
        }
      });
    }

    document.addEventListener(
      "keydown",
      function (event) {
        if (!(event.ctrlKey || event.metaKey) || event.altKey) {
          return;
        }
        if (event.key !== "f" && event.key !== "F") {
          return;
        }
        // Ctrl+F is claimed only for the terminal. Anywhere else on the page —
        // the Copilot textarea, a settings field — the browser's own find is
        // still the right behaviour, and taking it would be obnoxious.
        var target = event.target;
        var insideTerminal =
          target && typeof target.closest === "function" && target.closest(".terminal-shell");
        if (!insideTerminal && !isOpen() && target !== document.body) {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        open();
      },
      true
    );

    // A screen refresh invalidates every match position.
    grid().onScreenReplaced(function () {
      if (isOpen()) {
        run();
      }
    });
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.screenFind = {
    open: open,
    close: close,
    isOpen: isOpen,
    find: function (term) {
      open();
      if (input) {
        input.value = term || "";
      }
      run();
      return matches.length;
    },
    next: function () {
      step(1);
    },
    previous: function () {
      step(-1);
    },
    matchCount: function () {
      return matches.length;
    }
  };
})();
