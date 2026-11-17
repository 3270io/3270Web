// Applies this browser's own choice about the virtual keypad, as early as the
// page allows.
//
// View > Virtual keypad is a personal choice, so it is remembered by the
// browser and not by the server: the server's value (Settings > Interface >
// "Use keypad") is only the default for people who have not chosen. The page
// is rendered with that default; when this browser has a stored choice it
// replaces it here.
//
// It runs as a plain script directly after #keypad is parsed, in the same way
// screen-status-tracking-init.js does for its toggle. Waiting for keyboard.js
// (deferred, so it runs after the whole page is parsed) would let the first
// paint show the default and then jump to the person's choice.
//
// The value is "1" (shown) or "0" (hidden); keyboard.js writes it when the
// toggle is used and reads nothing else, so the key and its two values are
// the only contract between the two files.
(function () {
  "use strict";
  try {
    var keypad = document.getElementById("keypad");
    if (!keypad) {
      return;
    }
    var stored = localStorage.getItem("3270Web.keypad");
    if (stored === "1") {
      keypad.hidden = false;
    } else if (stored === "0") {
      keypad.hidden = true;
    }
    // Anything else (nothing stored, or storage unavailable and so thrown
    // above) leaves what the server rendered, which is the instance default.
  } catch (err) {
    // Storage can be blocked or empty (private windows, cleared site data);
    // the page then simply shows the default.
  }
})();
