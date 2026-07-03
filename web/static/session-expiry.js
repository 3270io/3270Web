// Shared session-expiry banner. Several independent background polls
// (workflow status, chaos status) previously handled a 401/403 from the
// host session by silently doing nothing forever, so a reaped/expired
// session gave no visible sign anything was wrong — the screen would just
// stop updating. Exposes window.ThreeSeventyWeb.notifySessionExpired(),
// which every 401/403-handling call site should call instead of swallowing
// the error; it shows a single persistent banner (not one per poll tick)
// with a Reconnect action.
(function () {
  "use strict";

  var shown = false;

  function notifySessionExpired() {
    if (shown) {
      return;
    }
    shown = true;
    if (!(window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function")) {
      return;
    }
    window.ThreeSeventyWeb.notify(
      "Your session has expired or the connection was lost. Reconnect to continue.",
      "error",
      {
        duration: 0,
        action: {
          label: "Reconnect",
          onClick: function () {
            window.location.href = "/";
          }
        }
      }
    );
  }

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.notifySessionExpired = notifySessionExpired;
})();
