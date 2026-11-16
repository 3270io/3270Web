/* The connect page's way out of a refusal.
 *
 * Sample port. Every bundled sample app has a port of its own, and the picker's
 * port select follows the app chosen rather than staying on one number for all
 * of them, which is how a second sample used to collide with the first.
 */
(function () {
  "use strict";

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

  setupSamplePort();
})();
