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

  function render() {
    if (!bar) {
      return;
    }
    // One session is not a tab bar, it is clutter. The bar appears when a
    // second session does.
    if (sessions.length < 2) {
      bar.hidden = true;
      bar.innerHTML = "";
      renderNewButton();
      return;
    }
    bar.hidden = false;
    bar.innerHTML = "";

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

      bar.appendChild(tab);
    }

    renderNewButton();
  }

  function renderNewButton() {
    var trigger = document.querySelector("[data-session-new]");
    if (!trigger) {
      return;
    }
    var full = sessions.length >= maxSessions;
    trigger.disabled = full;
    var hint = full
      ? "Maximum of " + maxSessions + " sessions already open"
      : "Open another host session";
    trigger.setAttribute("title", hint);
    trigger.setAttribute("aria-label", hint);
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
  // Where connection profiles exist, they are the answer: a profile carries
  // TLS, LU, model and code page, none of which a typed hostname can express.
  // The prompt is the fallback for a deployment that has not set any up.
  function openNew() {
    if (busy) {
      return;
    }
    var picker = window.ThreeSeventyWeb && window.ThreeSeventyWeb.connectionProfiles;
    if (picker) {
      picker
        .load()
        .then(function (available) {
          if (available && available.length) {
            picker.pick(function (profile) {
              connect({ profile: profile.name }, profile.name);
            });
            return;
          }
          promptForHost();
        })
        .catch(promptForHost);
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

  document.addEventListener("DOMContentLoaded", function () {
    bar = document.querySelector("[data-session-tabs]");
    if (!bar) {
      return;
    }

    var trigger = document.querySelector("[data-session-new]");
    if (trigger) {
      trigger.addEventListener("click", openNew);
    }

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
    list: function () {
      return sessions.slice();
    }
  };
})();
