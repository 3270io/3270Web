// Connection loss and recovery.
//
// Mainframe sessions drop for routine reasons — LPAR maintenance, VTAM
// timeouts, a network blip — and until now the only recovery was to go back
// to the connect page and retype the host, losing your place several times a
// day. Every desktop emulator reconnects on its own; this does too.
//
// Exposes window.ThreeSeventyWeb.notifySessionExpired(), which every
// 401/403-handling call site calls instead of swallowing the error. The first
// call starts an automatic retry sequence with backoff; if that gives up, a
// persistent banner offers a manual Reconnect.
(function () {
  "use strict";

  // Backoff schedule in milliseconds. Front-loaded because the overwhelming
  // majority of real drops recover within a couple of seconds, and slow
  // enough at the tail not to hammer a host that is genuinely down.
  var RETRY_DELAYS = [1000, 2000, 4000, 8000, 15000];

  var recovering = false;
  var bannerShown = false;
  var attempt = 0;
  var timer = 0;

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      return window.ThreeSeventyWeb.notify(message, type, options);
    }
    return null;
  }

  function setReconnectButtonVisible(visible) {
    var button = document.querySelector("[data-reconnect]");
    if (button) {
      button.hidden = !visible;
    }
  }

  function markDisconnected() {
    var indicator = document.querySelector("[data-oia-indicator]");
    if (indicator) {
      indicator.textContent = "X DISCONNECTED";
      indicator.setAttribute("data-oia-state", "error");
      indicator.setAttribute("title", "Connection to the host was lost");
    }
    var online = document.querySelector("[data-oia-online]");
    if (online) {
      online.textContent = "–";
    }
    var announce = document.querySelector("[data-oia-announce]");
    if (announce) {
      announce.textContent = "Connection to the host was lost";
    }
    setReconnectButtonVisible(true);
  }

  // reconnect re-dials the session's last target. On success the page is
  // reloaded rather than patched: the session ID, cookie and every piece of
  // per-session state on the page belong to the connection that just died,
  // and reusing them would leave the UI quietly lying about what it is
  // attached to.
  function reconnect() {
    return fetch("/reconnect", {
      method: "POST",
      headers: { Accept: "application/json" },
      credentials: "same-origin"
    }).then(function (response) {
      if (!response.ok) {
        return response
          .json()
          .catch(function () {
            return {};
          })
          .then(function (payload) {
            throw new Error(payload.error || "reconnect failed");
          });
      }
      return true;
    });
  }

  function scheduleRetry() {
    if (attempt >= RETRY_DELAYS.length) {
      showManualBanner();
      return;
    }
    var delay = RETRY_DELAYS[attempt];
    attempt += 1;
    timer = window.setTimeout(function () {
      reconnect().then(
        function () {
          notify("Reconnected to the host.", "success");
          window.location.reload();
        },
        function () {
          scheduleRetry();
        }
      );
    }, delay);
  }

  function showManualBanner() {
    if (bannerShown) {
      return;
    }
    bannerShown = true;
    notify(
      "Lost the connection to the host and automatic reconnection did not succeed.",
      "error",
      {
        duration: 0,
        action: {
          label: "Reconnect",
          onClick: function () {
            manualReconnect();
          }
        }
      }
    );
  }

  function manualReconnect() {
    notify("Reconnecting…", "info");
    reconnect().then(
      function () {
        window.location.reload();
      },
      function (err) {
        notify(
          (err && err.message) || "Reconnect failed. The host may still be unavailable.",
          "error"
        );
      }
    );
  }

  function notifySessionExpired() {
    if (recovering) {
      return;
    }
    recovering = true;
    markDisconnected();
    notify("Connection lost — reconnecting…", "warning", { duration: 4000 });
    attempt = 0;
    scheduleRetry();
  }

  // A drop noticed while the tab was in the background has usually already
  // resolved by the time the operator looks at it, so retry immediately on
  // focus rather than waiting out the remaining backoff.
  document.addEventListener("visibilitychange", function () {
    if (document.visibilityState !== "visible" || !recovering || bannerShown) {
      return;
    }
    if (timer) {
      window.clearTimeout(timer);
      timer = 0;
    }
    attempt = 0;
    scheduleRetry();
  });

  document.addEventListener("DOMContentLoaded", function () {
    var button = document.querySelector("[data-reconnect]");
    if (button) {
      button.addEventListener("click", function () {
        manualReconnect();
      });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.notifySessionExpired = notifySessionExpired;
  window.ThreeSeventyWeb.reconnect = manualReconnect;
})();
