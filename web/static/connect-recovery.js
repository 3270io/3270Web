/* The connect page's two ways out of a refusal.
 *
 * Session limit. When Connect is refused because the account already holds
 * every session it is allowed, the panel lists those sessions (the server draws
 * the list; see the session-limit block in connect.html) and this wires Resume
 * and Close to the same /sessions endpoints the terminal's tab bar uses.
 *
 * Sample port. Every bundled sample app has a port of its own, and the picker's
 * port select follows the app chosen rather than staying on one number for all
 * of them, which is how a second sample used to collide with the first.
 */
(function () {
  "use strict";

  function notify(message, type) {
    var api = window.ThreeSeventyWeb;
    if (api && typeof api.notify === "function") {
      api.notify(message, type || "info");
    }
  }

  function post(url, params) {
    return fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        Accept: "application/json"
      },
      credentials: "same-origin",
      body: new URLSearchParams(params || {}).toString()
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

  function setupSessionLimit() {
    var panel = document.querySelector("[data-session-limit]");
    if (!panel) {
      return;
    }
    var status = panel.querySelector("[data-session-limit-status]");
    var busy = false;

    function say(message) {
      if (status) {
        status.textContent = message;
      }
    }

    function nameOf(row) {
      var name = row.querySelector(".session-limit-name");
      var text = name && name.firstChild ? name.firstChild.textContent.trim() : "";
      return text || "the session";
    }

    function resume(row) {
      busy = true;
      say("Opening " + nameOf(row) + "…");
      post("/sessions/switch", { id: row.dataset.sessionId }).then(
        function () {
          window.location.href = "/screen";
        },
        function (err) {
          busy = false;
          say("");
          notify((err && err.message) || "Could not resume that session.", "error");
        }
      );
    }

    function close(row, button) {
      busy = true;
      var name = nameOf(row);
      button.disabled = true;
      post("/sessions/close", { id: row.dataset.sessionId }).then(
        function () {
          busy = false;
          var next = row.nextElementSibling || row.previousElementSibling;
          row.remove();
          // Focus would otherwise drop to the top of the page with the button
          // that had it. Move it to what is now first in the list, or to the
          // address field once the list is empty.
          var target = next && next.querySelector("button");
          if (!target) {
            target = document.getElementById("hostname-input");
          }
          if (target) {
            target.focus();
          }
          say("Closed " + name + ". There is room for another session now: press Connect to try again.");
        },
        function (err) {
          busy = false;
          button.disabled = false;
          say("");
          notify((err && err.message) || "Could not close " + name + ".", "error");
        }
      );
    }

    panel.addEventListener("click", function (event) {
      var button = event.target.closest && event.target.closest("button");
      var row = button && button.closest("[data-session-id]");
      if (!button || !row || busy) {
        return;
      }
      if (button.hasAttribute("data-session-resume")) {
        resume(row);
      } else if (button.hasAttribute("data-session-close")) {
        close(row, button);
      }
    });
  }

  function setupSamplePort() {
    var app = document.getElementById("sample-app-select");
    var port = document.getElementById("sample-app-port");
    if (!app || !port) {
      return;
    }
    function follow() {
      var chosen = app.options[app.selectedIndex];
      var wanted = chosen && chosen.getAttribute("data-port");
      if (!wanted) {
        return;
      }
      for (var i = 0; i < port.options.length; i++) {
        if (port.options[i].value === wanted) {
          port.selectedIndex = i;
          return;
        }
      }
    }
    app.addEventListener("change", follow);
    follow();
  }

  setupSessionLimit();
  setupSamplePort();
})();
