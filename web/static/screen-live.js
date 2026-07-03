// Idle screen push — keeps the terminal in sync with screen changes that
// didn't originate from this browser's own keystroke/submit (e.g. an
// unsolicited host update, or a Copilot tool call), instead of only
// refreshing on the user's next local interaction. Backed by a persistent
// Server-Sent Events connection (see ScreenStreamHandler in
// cmd/3270Web/screen_stream.go), which itself yields entirely to the
// existing chaos/playback polling in workflow.js while either is running
// for this session.
(function () {
  "use strict";

  // Focus normally rests inside .screen-container by default (a field is
  // auto-focused on load), so "is something focused in there" can't be the
  // skip signal the way it is for workflow.js's chaos/playback poller.
  // What actually must not be clobbered is an in-progress, not-yet-submitted
  // keystroke — 3270 fields are local until Enter/PF is pressed — so the
  // guard here is recency of actual typing instead.
  var RECENT_INPUT_GRACE_MS = 1500;
  var lastLocalInputAt = 0;

  function getFocusedFieldInfo(container) {
    var active = document.activeElement;
    if (!active || !container.contains(active)) {
      return null;
    }
    var caret = typeof active.selectionStart === "number" ? active.selectionStart : null;
    return { name: active.name || null, caret: caret };
  }

  function applyUpdate(container, payload) {
    if (!payload || typeof payload.html !== "string") {
      return;
    }
    if (Date.now() - lastLocalInputAt < RECENT_INPUT_GRACE_MS) {
      // The user's own next keystroke/submit will pick up the fresh state.
      return;
    }

    var prior = getFocusedFieldInfo(container);
    container.innerHTML = payload.html;
    if (typeof window.updateScreenStatusLine === "function") {
      window.updateScreenStatusLine(payload);
    }
    var form = container.querySelector("form.renderer-form");
    var formId = form ? (form.id || form.getAttribute("name")) : null;
    if (typeof window.installKeyHandler === "function") {
      window.installKeyHandler(formId);
    }
    if (form && typeof window.restoreScreenFocus === "function") {
      window.restoreScreenFocus(
        form,
        prior && prior.name,
        prior && prior.caret,
        payload.cursorRow,
        payload.cursorCol
      );
    }
    if (typeof window.sizeScreenContainer === "function") {
      window.sizeScreenContainer();
    }
  }

  document.addEventListener("DOMContentLoaded", function () {
    var container = document.querySelector(".screen-container");
    if (!container || typeof window.EventSource !== "function") {
      return;
    }

    container.addEventListener(
      "input",
      function () {
        lastLocalInputAt = Date.now();
      },
      true
    );

    var source = new EventSource("/screen/stream");
    source.onmessage = function (event) {
      var payload = null;
      try {
        payload = JSON.parse(event.data);
      } catch (_) {
        return;
      }
      applyUpdate(container, payload);
    };
    source.onerror = function () {
      // A transient network blip leaves readyState CONNECTING and the
      // browser retries on its own — nothing to do. readyState CLOSED means
      // the browser gave up for good, which happens when the initial (or a
      // reconnect) response wasn't a 200 text/event-stream — in practice,
      // an expired/reaped host session returning 401.
      if (
        source.readyState === EventSource.CLOSED &&
        window.ThreeSeventyWeb &&
        typeof window.ThreeSeventyWeb.notifySessionExpired === "function"
      ) {
        window.ThreeSeventyWeb.notifySessionExpired();
      }
    };
  });
})();
