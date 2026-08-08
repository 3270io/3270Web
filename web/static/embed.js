// The bridge between a terminal in an iframe and the page around it.
//
// The frame and its parent are different origins, so the parent cannot reach
// into the document, and the document cannot call out. postMessage is the
// only channel, and this is the whole of what travels on it: a small set of
// named commands, each answered with a result carrying the id the caller
// sent, so a parent can await a reply rather than guess when one arrived.
//
// Two rules make it safe to leave switched on. Every inbound message must
// come from an origin the operator named in EMBED_ORIGINS — the frame-ancestors
// header decides who may frame the terminal, and this decides who may drive
// it, and they are deliberately the same list. And nothing here is a new
// capability: each command calls the same same-origin endpoint the toolbar
// calls, so a parent page can do exactly what the person looking at the frame
// could already do, and nothing beyond it.
(function () {
  "use strict";

  var PROTOCOL = "3270web";

  function allowedOrigins() {
    var raw = (document.body && document.body.getAttribute("data-embed-origins")) || "";
    return raw.split(/\s+/).filter(function (o) {
      return o.length > 0;
    });
  }

  function isFramed() {
    try {
      return window.parent && window.parent !== window;
    } catch (_) {
      // A cross-origin parent can throw on access in some browsers; that it
      // threw is itself the answer.
      return true;
    }
  }

  // post sends one message to every allowed origin rather than to "*".
  // Targeting "*" would deliver the screen's contents to whatever page turned
  // out to be the parent, which is the one thing this file exists to prevent.
  function post(origins, payload) {
    if (!isFramed()) {
      return;
    }
    payload.source = PROTOCOL;
    for (var i = 0; i < origins.length; i++) {
      try {
        window.parent.postMessage(payload, origins[i]);
      } catch (_) {
        // A parent that has gone away is not an error worth reporting to a
        // page that cannot see it either.
      }
    }
  }

  function csrfSafeFetch(url, options) {
    var opts = options || {};
    opts.credentials = "same-origin";
    opts.headers = opts.headers || {};
    if (opts.body && !opts.headers["Content-Type"]) {
      opts.headers["Content-Type"] = "application/json";
    }
    return fetch(url, opts).then(function (response) {
      return response
        .json()
        .catch(function () {
          return {};
        })
        .then(function (body) {
          if (!response.ok) {
            var message = (body && body.error) || "request failed with status " + response.status;
            throw new Error(message);
          }
          return body;
        });
    });
  }

  // COMMANDS is the entire vocabulary. Adding to it is adding to what a
  // parent page can do, so each entry names the endpoint it calls and
  // nothing composes two of them behind one name.
  var COMMANDS = {
    // Read the screen as JSON — text, fields, cursor, keyboard state.
    getScreen: function () {
      return csrfSafeFetch("/screen.json");
    },
    // Send one AID or navigation key: "Enter", "PF3", "Tab", "Clear".
    sendKey: function (msg) {
      return csrfSafeFetch("/screen/key", {
        method: "POST",
        body: JSON.stringify({ key: String(msg.key || "") }),
      });
    },
    // Write text into the field containing (row, col), 0-indexed as the
    // screen JSON reports them.
    writeField: function (msg) {
      return csrfSafeFetch("/screen/write", {
        method: "POST",
        body: JSON.stringify({
          row: Number(msg.row) || 0,
          col: Number(msg.col) || 0,
          text: String(msg.text || ""),
        }),
      });
    },
    // Submit the fields written so far with an AID key (Enter by default).
    submit: function (msg) {
      return csrfSafeFetch("/screen/submit", {
        method: "POST",
        body: JSON.stringify({ aid: String(msg.aid || "Enter") }),
      });
    },
    // Move the cursor without sending anything to the host.
    moveCursor: function (msg) {
      return csrfSafeFetch("/screen/cursor", {
        method: "POST",
        body: JSON.stringify({ row: Number(msg.row) || 0, col: Number(msg.col) || 0 }),
      });
    },
    // Wait for the keyboard to unlock, i.e. for the host to finish.
    waitForUnlock: function () {
      return csrfSafeFetch("/screen/wait", { method: "POST", body: JSON.stringify({}) });
    },
    // A liveness check that touches no host state, so an integrator can
    // confirm the channel before wiring anything to it.
    ping: function () {
      return Promise.resolve({ ok: true });
    },
  };

  function handleMessage(origins, event) {
    if (origins.indexOf(event.origin) === -1) {
      return;
    }
    var msg = event.data;
    if (!msg || typeof msg !== "object" || msg.source === PROTOCOL) {
      // Ignore our own outbound messages, which a parent that re-broadcasts
      // would otherwise send straight back.
      return;
    }
    var command = COMMANDS[msg.type];
    if (!command) {
      post(origins, {
        type: "result",
        id: msg.id === undefined ? null : msg.id,
        ok: false,
        error: "unknown command " + JSON.stringify(msg.type),
        commands: Object.keys(COMMANDS),
      });
      return;
    }
    var pending;
    try {
      pending = Promise.resolve(command(msg));
    } catch (err) {
      pending = Promise.reject(err);
    }
    pending.then(
      function (data) {
        post(origins, { type: "result", id: msg.id === undefined ? null : msg.id, ok: true, data: data });
      },
      function (err) {
        post(origins, {
          type: "result",
          id: msg.id === undefined ? null : msg.id,
          ok: false,
          error: (err && err.message) || String(err),
        });
      }
    );
  }

  document.addEventListener("DOMContentLoaded", function () {
    var origins = allowedOrigins();
    if (!isFramed() || origins.length === 0) {
      // Not framed, or framing was never configured: there is nobody to talk
      // to, and no origin it would be safe to talk to if there were.
      return;
    }

    window.addEventListener("message", function (event) {
      handleMessage(origins, event);
    });

    // The host can repaint without anyone here asking it to, and a parent
    // that only ever saw replies to its own commands would miss exactly those
    // screens. screen-live.js already receives them; this forwards them on.
    document.addEventListener("3270web:screen-updated", function (event) {
      var detail = event.detail || {};
      post(origins, {
        type: "screen",
        cursorRow: detail.cursorRow,
        cursorCol: detail.cursorCol,
        status: detail.status,
      });
    });

    post(origins, {
      type: "ready",
      session: document.body.getAttribute("data-session-id") || null,
      embedded: document.body.getAttribute("data-embedded") === "true",
      commands: Object.keys(COMMANDS),
    });
  });
})();
