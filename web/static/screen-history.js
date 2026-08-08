// Screen history (scrollback).
//
// A 3270 screen is repainted in place, so once the host moves on, whatever was
// there is gone. "What did the previous screen say?" — the account number on
// the confirmation you just tabbed past, the error text that flashed by — was
// simply unanswerable, and every desktop emulator keeps a history.
//
// The server keeps the last N screens per session as plain text (see
// Session.RecordScreen). This browses them read-only: you cannot type into a
// screen from five minutes ago, and pretending otherwise would be worse than
// not offering it.
(function () {
  "use strict";

  var screens = [];
  var index = -1;
  var modal = null;
  var view = null;
  var label = null;
  var prevBtn = null;
  var nextBtn = null;

  function notify(message, type) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type);
    }
  }

  function formatTime(iso) {
    if (!iso) {
      return "";
    }
    var d = new Date(iso);
    if (isNaN(d.getTime())) {
      return "";
    }
    return d.toLocaleTimeString();
  }

  function render() {
    if (!view) {
      return;
    }
    if (!screens.length) {
      view.textContent = "";
      if (label) {
        label.textContent = "No history yet";
      }
      if (prevBtn) prevBtn.disabled = true;
      if (nextBtn) nextBtn.disabled = true;
      return;
    }
    var entry = screens[index];
    view.textContent = entry.text || "";
    if (label) {
      // Counted back from the present, which is how someone thinks about it:
      // "the one before last", not "number 37 of 50".
      var stepsBack = screens.length - 1 - index;
      var position =
        stepsBack === 0 ? "Most recent" : stepsBack + (stepsBack === 1 ? " screen back" : " screens back");
      var stamp = formatTime(entry.at);
      label.textContent = position + (stamp ? " · " + stamp : "") + " · " + (index + 1) + " of " + screens.length;
    }
    if (prevBtn) prevBtn.disabled = index <= 0;
    if (nextBtn) nextBtn.disabled = index >= screens.length - 1;
  }

  function step(delta) {
    if (!screens.length) {
      return;
    }
    var next = index + delta;
    if (next < 0 || next >= screens.length) {
      return;
    }
    index = next;
    render();
  }

  function load() {
    return fetch("/screen/history", {
      headers: { Accept: "application/json", "Cache-Control": "no-cache" },
      credentials: "same-origin"
    })
      .then(function (response) {
        if (response.status === 401 || response.status === 403) {
          if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notifySessionExpired === "function") {
            window.ThreeSeventyWeb.notifySessionExpired();
          }
          throw new Error("session expired");
        }
        if (!response.ok) {
          throw new Error("history unavailable");
        }
        return response.json();
      })
      .then(function (payload) {
        screens = payload && Array.isArray(payload.screens) ? payload.screens : [];
        index = screens.length - 1;
      });
  }

  function open() {
    if (!modal) {
      return;
    }
    load().then(
      function () {
        modal.hidden = false;
        render();
        var closeBtn = modal.querySelector("[data-history-close]");
        if (closeBtn) {
          closeBtn.focus();
        }
      },
      function (err) {
        if (err && err.message !== "session expired") {
          notify("Could not load screen history.", "error");
        }
      }
    );
  }

  function close() {
    if (!modal) {
      return;
    }
    modal.hidden = true;
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.terminal) {
      window.ThreeSeventyWeb.terminal.focusTerminal();
    }
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  function copyCurrent() {
    if (!screens.length) {
      return;
    }
    var text = screens[index].text || "";
    var done = function (ok) {
      notify(ok ? "Screen copied to clipboard" : "Copy failed", ok ? "success" : "error");
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () {
          done(true);
        },
        function () {
          done(false);
        }
      );
      return;
    }
    done(false);
  }

  function build() {
    modal = document.createElement("div");
    modal.className = "history-modal";
    modal.hidden = true;
    modal.setAttribute("data-history-modal", "");
    modal.innerHTML =
      '<div class="history-modal-backdrop" data-history-close></div>' +
      '<div class="history-modal-content" role="dialog" aria-modal="true" aria-labelledby="history-modal-title">' +
      '  <div class="history-modal-header">' +
      '    <h3 id="history-modal-title">Screen history</h3>' +
      '    <div class="history-modal-actions">' +
      '      <button type="button" data-history-prev aria-label="Older screen">&#8592; Older</button>' +
      '      <button type="button" data-history-next aria-label="Newer screen">Newer &#8594;</button>' +
      '      <button type="button" data-history-copy>Copy</button>' +
      '      <button type="button" data-history-close>Close</button>' +
      "    </div>" +
      "  </div>" +
      '  <div class="history-modal-meta" data-history-label aria-live="polite"></div>' +
      '  <pre class="history-modal-view" data-history-view tabindex="0"></pre>' +
      '  <div class="history-modal-note subtle">Read-only. Use &#8592; and &#8594; to move between screens.</div>' +
      "</div>";
    document.body.appendChild(modal);

    view = modal.querySelector("[data-history-view]");
    label = modal.querySelector("[data-history-label]");
    prevBtn = modal.querySelector("[data-history-prev]");
    nextBtn = modal.querySelector("[data-history-next]");

    prevBtn.addEventListener("click", function () {
      step(-1);
    });
    nextBtn.addEventListener("click", function () {
      step(1);
    });
    modal.querySelector("[data-history-copy]").addEventListener("click", copyCurrent);
    var closers = modal.querySelectorAll("[data-history-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }

    // Escape and the arrows are handled at the document, not on the dialog:
    // focus is not reliably inside it. Opening is asynchronous (the history
    // is fetched first), and the terminal's focus lock can pull focus back to
    // the screen in the gap, which left Escape doing nothing. keyboard.js
    // already declines to act while a modal is open, so nothing competes.
    document.addEventListener(
      "keydown",
      function (event) {
        if (!isOpen()) {
          return;
        }
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          close();
          return;
        }
        if (event.key === "ArrowLeft") {
          event.preventDefault();
          event.stopPropagation();
          step(-1);
          return;
        }
        if (event.key === "ArrowRight") {
          event.preventDefault();
          event.stopPropagation();
          step(1);
        }
      },
      true
    );
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".screen-container")) {
      return;
    }
    build();

    var trigger = document.querySelector("[data-history-open]");
    if (trigger) {
      trigger.addEventListener("click", open);
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.screenHistory = {
    open: open,
    close: close,
    isOpen: isOpen,
    count: function () {
      return screens.length;
    }
  };
})();
