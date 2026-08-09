// Connection details: what the terminal knows about its link to the host.
//
// The screen tells you what the application is showing. It cannot tell you
// whether TN3270E negotiated, what the host bound the LU as, which telnet
// options are in force, or whether the TLS session is what the profile asked
// for. A session can render perfectly and still be connected on terms nobody
// chose. GET /host/query answers that, and this is where an operator on a
// support call reads it out.
//
// The field set is whatever the terminal reports, not a list hard-coded here —
// builds differ. So the panel promotes the fields people actually ask for into
// a short summary with readable labels, and puts *everything* under a
// disclosure in the terminal's own order. A field this file has never heard of
// still appears; it just appears in the second list.
(function () {
  "use strict";

  // Fields worth answering first, with the label an operator would use rather
  // than the terminal's own identifier.
  var SUMMARY = [
    ["ConnectionState", "State"],
    ["Host", "Host"],
    ["TerminalName", "Terminal"],
    ["ScreenSizeCurrent", "Screen size"],
    ["CodePage", "Code page"],
    ["Tls", "TLS"],
    ["LuName", "LU name"],
    ["BindPluName", "Bound PLU"],
    ["Tn3270eOptions", "TN3270E options"],
    ["TelnetHostOptions", "Host telnet options"],
    ["TelnetMyOptions", "Our telnet options"],
    ["ConnectTime", "Connected for"],
    ["StatsRx", "Received"],
    ["StatsTx", "Sent"]
  ];

  // What the terminal puts in place of its longest values in the bulk answer,
  // meaning "ask for this one by name". Mirrors elidedQueryValue in Go.
  var ELIDED = "...";

  // A value the terminal reports as empty is a fact, not a gap: "the host bound
  // no LU name" and "this build cannot tell you" are different answers and the
  // panel must not blur them.
  var EMPTY_LABEL = "— none reported —";

  document.addEventListener("DOMContentLoaded", function () {
    var modal = document.querySelector("[data-host-details-modal]");
    if (!modal) {
      return;
    }

    var statusEl = modal.querySelector("[data-host-details-status]");
    var summaryEl = modal.querySelector("[data-host-details-summary]");
    var allWrap = modal.querySelector("[data-host-details-all-wrap]");
    var allEl = modal.querySelector("[data-host-details-all]");
    var refreshBtn = modal.querySelector("[data-host-details-refresh]");
    var copyBtn = modal.querySelector("[data-host-details-copy]");
    var loaded = null;

    function setStatus(text, isError) {
      statusEl.textContent = text || "";
      statusEl.hidden = !text;
      if (isError) {
        statusEl.setAttribute("data-error", "1");
      } else {
        statusEl.removeAttribute("data-error");
      }
    }

    function row(list, label, value, name) {
      var dt = document.createElement("dt");
      dt.textContent = label;
      var dd = document.createElement("dd");
      if (value === ELIDED) {
        // Fetch it by name, which is the only way to get these — and safe,
        // because the name came from the terminal's own list.
        var show = document.createElement("button");
        show.type = "button";
        show.className = "host-details-expand";
        show.textContent = "Show";
        show.addEventListener("click", function () {
          show.disabled = true;
          show.textContent = "Loading…";
          fetchQuery("?name=" + encodeURIComponent(name))
            .then(function (data) {
              dd.textContent = data.value || EMPTY_LABEL;
            })
            .catch(function (err) {
              show.disabled = false;
              show.textContent = "Show";
              setStatus(err.message, true);
            });
        });
        dd.appendChild(show);
      } else if (value === "") {
        dd.textContent = EMPTY_LABEL;
        dd.setAttribute("data-empty", "1");
      } else {
        dd.textContent = value;
      }
      list.appendChild(dt);
      list.appendChild(dd);
    }

    function fetchQuery(suffix) {
      return fetch("/host/query" + (suffix || ""), {
        headers: { Accept: "application/json" }
      }).then(function (res) {
        return res
          .json()
          .catch(function () {
            return {};
          })
          .then(function (body) {
            if (!res.ok) {
              throw new Error(body.error || "Could not read the connection details.");
            }
            return body;
          });
      });
    }

    function render(data) {
      loaded = data;
      var queries = data.queries || {};
      var available = data.available || [];

      summaryEl.textContent = "";
      var shown = 0;
      for (var i = 0; i < SUMMARY.length; i++) {
        var name = SUMMARY[i][0];
        if (!Object.prototype.hasOwnProperty.call(queries, name)) {
          continue;
        }
        row(summaryEl, SUMMARY[i][1], queries[name], name);
        shown++;
      }

      allEl.textContent = "";
      for (var j = 0; j < available.length; j++) {
        row(allEl, available[j], queries[available[j]], available[j]);
      }
      allWrap.hidden = available.length === 0;

      if (!available.length) {
        setStatus("This connection reported no details.", false);
      } else if (!shown) {
        // A build whose field names we do not recognise at all: the summary is
        // empty but the full list is not, and saying so beats a blank panel.
        setStatus("No recognised summary fields — see the full list below.", false);
      } else {
        setStatus("", false);
      }
    }

    function load() {
      setStatus("Loading…", false);
      fetchQuery("")
        .then(render)
        .catch(function (err) {
          summaryEl.textContent = "";
          allEl.textContent = "";
          allWrap.hidden = true;
          setStatus(err.message, true);
        });
    }

    // Copy is for pasting into a support ticket, so it copies what is on
    // screen as plain text rather than the JSON behind it.
    function asText() {
      if (!loaded) {
        return "";
      }
      var queries = loaded.queries || {};
      var lines = [];
      var available = loaded.available || [];
      for (var i = 0; i < available.length; i++) {
        var name = available[i];
        var value = queries[name];
        lines.push(name + ": " + (value === "" ? "(none)" : value));
      }
      return lines.join("\n");
    }

    // Focus, the Tab trap, the background scroll lock and the focus restore
    // all belong to pushModal/popModal — see modal-utils.js.
    function closeModal() {
      modal.hidden = true;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
    }

    function openModal() {
      modal.hidden = false;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
        window.ThreeSeventyWeb.pushModal(modal, closeModal);
      }
      // Read it fresh every time. These values change under the session —
      // byte counts always, and connection state exactly when it matters.
      load();
    }

    var openButtons = document.querySelectorAll("[data-host-details-open]");
    for (var i = 0; i < openButtons.length; i++) {
      openButtons[i].addEventListener("click", openModal);
    }
    var closeButtons = modal.querySelectorAll("[data-host-details-close]");
    for (var k = 0; k < closeButtons.length; k++) {
      closeButtons[k].addEventListener("click", closeModal);
    }
    if (refreshBtn) {
      refreshBtn.addEventListener("click", load);
    }
    if (copyBtn) {
      copyBtn.addEventListener("click", function () {
        var text = asText();
        if (!text) {
          return;
        }
        var done = function () {
          var was = copyBtn.textContent;
          copyBtn.textContent = "Copied";
          window.setTimeout(function () {
            copyBtn.textContent = was;
          }, 1200);
        };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(text).then(done, function () {
            setStatus("The browser refused clipboard access.", true);
          });
          return;
        }
        setStatus("This browser has no clipboard access.", true);
      });
    }

    modal.addEventListener("click", function (event) {
      if (event.target === modal) {
        closeModal();
      }
    });
  });
})();
