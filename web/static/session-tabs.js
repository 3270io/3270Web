// Session tabs.
//
// A business user routinely keeps three to six host sessions open — one for
// the application they work in, one for a lookup, one for TSO — and until now
// a browser could hold exactly one. Every desktop emulator is tabbed.
//
// Switching a tab reloads the page rather than swapping the screen in place.
// That is deliberate: a session owns not just the screen but the OIA, the
// recording and chaos state, the SSE stream and the Copilot conversation, and
// a full reload is the one way to guarantee none of the outgoing session's
// state is left pointing at the incoming one. Sessions stay live on the
// server across the reload, so nothing is lost by it.
(function () {
  "use strict";

  var bar = null;
  var list = null;
  var sessions = [];
  var maxSessions = 6;
  var busy = false;

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type, options);
    }
  }

  function post(url, params) {
    var body = new URLSearchParams(params || {});
    return fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        Accept: "application/json"
      },
      credentials: "same-origin",
      body: body.toString()
    }).then(function (response) {
      return response
        .json()
        .catch(function () {
          return {};
        })
        .then(function (payload) {
          if (!response.ok) {
            throw new Error(payload.error || "request failed");
          }
          return payload;
        });
    });
  }

  // The bar is drawn even for a single session. It is the only place the UI
  // says "sessions are a thing you can have more than one of", and it cannot
  // say that from behind a hidden attribute that only lifts once you already
  // have two.
  function render() {
    if (!bar || !list) {
      return;
    }
    bar.hidden = false;
    list.innerHTML = "";

    for (var i = 0; i < sessions.length; i++) {
      var s = sessions[i];
      var tab = document.createElement("div");
      tab.className = "session-tab" + (s.active ? " is-active" : "");
      tab.setAttribute("role", "presentation");

      var button = document.createElement("button");
      button.type = "button";
      button.className = "session-tab-label";
      button.textContent = s.label || s.target || "Session";
      button.title = s.target || "";
      button.setAttribute("aria-current", s.active ? "page" : "false");
      button.setAttribute(
        "aria-label",
        (s.active ? "Current session: " : "Switch to session: ") + (s.target || s.label)
      );
      (function (id, active) {
        button.addEventListener("click", function () {
          if (!active) {
            switchTo(id);
          }
        });
      })(s.id, s.active);
      tab.appendChild(button);

      // No × on a lone tab. Closing the only session is disconnecting, and
      // Disconnect is already sitting in the toolbar underneath saying so.
      if (sessions.length > 1) {
        var close = document.createElement("button");
        close.type = "button";
        close.className = "session-tab-close";
        close.innerHTML = "&times;";
        close.setAttribute("aria-label", "Close session " + (s.target || s.label));
        close.title = "Close this session";
        (function (id, label) {
          close.addEventListener("click", function (event) {
            event.stopPropagation();
            closeSession(id, label);
          });
        })(s.id, s.label || s.target);
        tab.appendChild(close);
      }

      list.appendChild(tab);
    }

    renderNewButton();
  }

  // Only the hint moves here — the button's visible words stay put, so its
  // accessible name does not change under a screen reader between one call
  // and the next.
  function renderNewButton() {
    var trigger = document.querySelector("[data-session-new]");
    if (!trigger) {
      return;
    }
    var full = sessions.length >= maxSessions;
    trigger.disabled = full;
    var hint = full
      ? "Maximum of " + maxSessions + " sessions already open"
      : "Open another host session (Alt+N)";
    trigger.setAttribute("title", hint);
    if (trigger.hasAttribute("data-tippy-content")) {
      trigger.setAttribute("data-tippy-content", hint);
    }
  }

  function load() {
    return fetch("/sessions", {
      headers: { Accept: "application/json", "Cache-Control": "no-cache" },
      credentials: "same-origin"
    })
      .then(function (response) {
        if (!response.ok) {
          throw new Error("sessions unavailable");
        }
        return response.json();
      })
      .then(function (payload) {
        sessions = payload && Array.isArray(payload.sessions) ? payload.sessions : [];
        if (payload && typeof payload.max === "number") {
          maxSessions = payload.max;
        }
        render();
        return sessions;
      });
  }

  function switchTo(id) {
    if (busy) {
      return;
    }
    busy = true;
    post("/sessions/switch", { id: id }).then(
      function () {
        window.location.href = "/screen";
      },
      function (err) {
        busy = false;
        notify((err && err.message) || "Could not switch session.", "error");
        load();
      }
    );
  }

  function closeSession(id, label) {
    if (busy) {
      return;
    }
    busy = true;
    post("/sessions/close", { id: id }).then(
      function (payload) {
        if (payload && payload.remaining > 0) {
          window.location.href = "/screen";
          return;
        }
        // That was the last one; there is no terminal left to show.
        window.location.href = "/";
      },
      function (err) {
        busy = false;
        notify((err && err.message) || "Could not close session " + label + ".", "error");
        load();
      }
    );
  }

  function connect(params, label) {
    busy = true;
    notify("Connecting to " + label + "…", "info", { duration: 2000 });
    post("/sessions/new", params).then(
      function () {
        window.location.href = "/screen";
      },
      function (err) {
        busy = false;
        notify((err && err.message) || "Could not open a session to " + label + ".", "error");
        load();
      }
    );
  }

  // openNew asks where to connect rather than assuming the current host —
  // opening a second window onto the same host is a real use case, but so is
  // opening a different one, and silently defaulting would hide the second.
  //
  // It always asks in the picker, which offers the connection profiles, the
  // bundled sample apps and a box to type an address into. It used to fall
  // through to a window.prompt whenever there were no profiles yet, which is
  // the state a fresh instance is in: the one thing that instance can connect
  // to is a sample app, and a sample app's address — "sampleapp:<id>" on a
  // port that is not 3270 — is not something the prompt could tell anybody.
  // A prompt is also silently refused inside a sandboxed frame, so an
  // embedded terminal had no second session at all.
  function openNew() {
    if (busy) {
      return;
    }
    var picker = window.ThreeSeventyWeb && window.ThreeSeventyWeb.connectionProfiles;
    if (picker && typeof picker.pick === "function") {
      picker.pick(function (choice) {
        // A profile is named, and the name is what the server wants: it
        // carries TLS, LU, model and code page. A sample app or a typed
        // address is a target and nothing more.
        if (choice && choice.name) {
          connect({ profile: choice.name }, choice.label || choice.name);
          return;
        }
        if (choice && choice.target) {
          connect({ hostname: choice.target }, choice.label || choice.target);
        }
      });
      return;
    }
    promptForHost();
  }

  function promptForHost() {
    var suggestion = "";
    for (var i = 0; i < sessions.length; i++) {
      if (sessions[i].active) {
        suggestion = sessions[i].target || "";
        break;
      }
    }
    var hostname = window.prompt("Open a session to which host?", suggestion);
    if (hostname === null) {
      return;
    }
    hostname = hostname.trim();
    if (!hostname) {
      return;
    }
    connect({ hostname: hostname }, hostname);
  }

  function activeSession() {
    for (var i = 0; i < sessions.length; i++) {
      if (sessions[i].active) {
        return sessions[i];
      }
    }
    return null;
  }

  // closeActive exists for the command palette, which has no tab to click.
  function closeActive() {
    var current = activeSession();
    if (current) {
      closeSession(current.id, current.label || current.target);
    }
  }

  // A keystroke aimed at a text box is typing, not a shortcut — but "is this
  // a text box" cannot be answered by the tag alone here. The 3270 screen is
  // built out of <input> elements: every unprotected field on it is one, so a
  // plain tag test suppresses these chords in the one place an operator
  // actually presses them, which is with the terminal focused.
  //
  // So the terminal is explicitly not typing for this purpose. Alt is not how
  // characters are entered into a 3270 field — the only Alt chords the host
  // gets are Alt+F1..F3 for the PA keys, and those are function keys. An
  // ordinary text box anywhere else on the page — the AI chat composer, a
  // settings field, the find box — is left alone.
  function isTypingTarget(target) {
    if (!target || !target.tagName) {
      return false;
    }
    var tag = target.tagName.toLowerCase();
    var editable = tag === "input" || tag === "textarea" || tag === "select" || !!target.isContentEditable;
    if (!editable) {
      return false;
    }
    if (typeof target.closest === "function" && target.closest(".terminal-shell")) {
      return false;
    }
    return true;
  }

  // Alt+N / Alt+W / Alt+1…9, the tabbed-emulator convention. Alt rather than
  // Ctrl because Ctrl+N, Ctrl+W and Ctrl+T belong to the browser and cannot
  // reliably be taken back, and because Alt is already the terminal's own
  // modifier here — Alt+F1..F3 are the PA keys, and those are function keys,
  // so no chord below collides with one.
  //
  // A user who has remapped the same chord to a host action in the keymap
  // editor wins: their binding is explicit and this one is a default.
  function claimedByKeymap(event) {
    var keymap = window.ThreeSeventyWeb && window.ThreeSeventyWeb.keymap;
    return !!(keymap && typeof keymap.resolve === "function" && keymap.resolve(event));
  }

  function onShortcut(event) {
    if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
      return;
    }
    if (isTypingTarget(event.target) || claimedByKeymap(event)) {
      return;
    }

    var key = event.key || "";
    var handled = false;

    if (key === "n" || key === "N") {
      handled = true;
      openNew();
    } else if (key === "w" || key === "W") {
      // Only when there is something to fall back to. Alt+W on a lone
      // session would be a disconnect wearing a close button's clothes.
      if (sessions.length > 1) {
        handled = true;
        closeActive();
      }
    } else if (key >= "1" && key <= "9") {
      var index = Number(key) - 1;
      if (index < sessions.length) {
        handled = true;
        if (!sessions[index].active) {
          switchTo(sessions[index].id);
        }
      }
    }

    if (handled) {
      event.preventDefault();
      event.stopPropagation();
    }
  }

  document.addEventListener("DOMContentLoaded", function () {
    bar = document.querySelector("[data-session-tabs]");
    if (!bar) {
      return;
    }
    list = bar.querySelector("[data-session-tab-list]") || bar;

    var trigger = document.querySelector("[data-session-new]");
    if (trigger) {
      trigger.addEventListener("click", openNew);
    }

    // Capture phase, ahead of the terminal's own key handling — the same
    // way focus mode claims Alt+Enter.
    document.addEventListener("keydown", onShortcut, true);

    load().catch(function () {
      /* The terminal still works without a tab bar; no need to shout. */
    });
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.sessionTabs = {
    reload: load,
    open: openNew,
    switchTo: switchTo,
    close: closeSession,
    closeActive: closeActive,
    list: function () {
      return sessions.slice();
    }
  };
})();
