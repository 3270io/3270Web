// Printer session: bind a 3287 LU beside this terminal, and collect what the
// host prints to it.
//
// The panel has to answer three questions at once, because an operator opening
// it may be in any of three situations: is a printer bound at all, is it still
// alive, and has anything arrived. So the status line is a sentence rather than
// a badge, and the job list is always visible — a printer that stopped an hour
// ago may still have the report somebody came here for.
//
// It polls while it is open and stops when it is closed. A print job arrives
// when the host decides, not when the browser asks, and a panel that only
// refreshed on a button would sit there saying "no jobs" over a directory with
// one in it.
(function () {
  "use strict";

  // POLL_MS is how often an open panel re-reads the status. Print jobs are not
  // interactive — nobody is waiting on a five-second boundary — and the request
  // costs a directory listing on the server.
  var POLL_MS = 5000;

  function bytesLabel(n) {
    if (typeof n !== "number" || n < 0) {
      return "";
    }
    if (n < 1024) {
      return n + " B";
    }
    if (n < 1024 * 1024) {
      return (n / 1024).toFixed(1) + " KB";
    }
    return (n / (1024 * 1024)).toFixed(1) + " MB";
  }

  function timeLabel(iso) {
    if (!iso) {
      return "";
    }
    var when = new Date(iso);
    if (isNaN(when.getTime())) {
      return "";
    }
    return when.toLocaleString();
  }

  document.addEventListener("DOMContentLoaded", function () {
    var modal = document.querySelector("[data-printer-modal]");
    if (!modal) {
      return;
    }

    var statusEl = modal.querySelector("[data-printer-status]");
    var formEl = modal.querySelector("[data-printer-form]");
    var fieldsEl = modal.querySelector("[data-printer-fields]");
    var luEl = modal.querySelector("[data-printer-lu]");
    var startBtn = modal.querySelector("[data-printer-start]");
    var stopBtn = modal.querySelector("[data-printer-stop]");
    var refreshBtn = modal.querySelector("[data-printer-refresh]");
    var jobsEl = modal.querySelector("[data-printer-jobs]");
    var bindRadios = modal.querySelectorAll("[data-printer-bind]");
    var timer = null;
    var busy = false;
    // The last failure the operator caused, held until they do something else.
    //
    // Without this the poll erases it: an operator clicks Start, the host
    // refuses the LU, the message naming the LU appears — and five seconds
    // later a refresh replaces it with "No printer session is bound", which is
    // true, useless, and looks like the click did nothing at all.
    var lastFailure = "";

    function setStatus(text, isError) {
      statusEl.textContent = text || "";
      statusEl.hidden = !text;
      if (isError) {
        statusEl.setAttribute("data-error", "1");
      } else {
        statusEl.removeAttribute("data-error");
      }
    }

    function bindMode() {
      for (var i = 0; i < bindRadios.length; i++) {
        if (bindRadios[i].checked) {
          return bindRadios[i].value;
        }
      }
      return "associate";
    }

    function syncBindFields() {
      var byName = bindMode() === "lu";
      luEl.disabled = !byName;
      if (!byName) {
        luEl.value = "";
      }
    }

    function numberFrom(selector) {
      var el = modal.querySelector(selector);
      if (!el) {
        return 0;
      }
      var value = parseInt(el.value, 10);
      return isNaN(value) ? 0 : value;
    }

    function checked(selector) {
      var el = modal.querySelector(selector);
      return !!(el && el.checked);
    }

    function textFrom(selector) {
      var el = modal.querySelector(selector);
      return el ? (el.value || "").trim() : "";
    }

    function startBody() {
      return {
        associate: bindMode() === "associate",
        lu: luEl.value.trim(),
        mpp: numberFrom("[data-printer-mpp]"),
        code_page: textFrom("[data-printer-codepage]"),
        eoj_timeout: numberFrom("[data-printer-eoj]"),
        crlf: checked("[data-printer-crlf]"),
        blank_lines: checked("[data-printer-blanklines]"),
        ff_skip: checked("[data-printer-ffskip]"),
        ff_thru: checked("[data-printer-ffthru]"),
        ignore_eoj: checked("[data-printer-ignoreeoj]"),
        skip_cc: checked("[data-printer-skipcc]")
      };
    }

    // A failed request should say what the server said. Everything here can
    // fail for a reason the operator can act on — no pr3287 installed, an LU
    // the host will not give up, a session that disconnected — so an
    // unexplained "something went wrong" would be the worst possible answer.
    function request(method, url, body) {
      var init = { method: method, credentials: "same-origin" };
      if (body) {
        init.headers = { "Content-Type": "application/json" };
        init.body = JSON.stringify(body);
      }
      return fetch(url, init).then(function (resp) {
        return resp.json().catch(function () {
          return {};
        }).then(function (data) {
          if (!resp.ok) {
            throw new Error(data.error || ("The server answered " + resp.status + "."));
          }
          return data;
        });
      });
    }

    function describe(state) {
      if (!state.available) {
        return "This installation has no pr3287 binary, so it cannot bind a printer LU.";
      }
      var printer = state.printer;
      if (!printer) {
        return "No printer session is bound.";
      }
      var which = printer.lu ? ("LU " + printer.lu)
        : (printer.associate ? ("the printer associated with " + printer.associate) : "a printer LU");
      if (printer.running) {
        return "Bound to " + which + " on " + printer.host + ":" + printer.port +
          (printer.tls ? " over TLS" : "") + ", waiting for jobs.";
      }
      if (printer.error) {
        return "The printer session for " + which + " stopped: " + printer.error;
      }
      return "The printer session for " + which + " is not running.";
    }

    function renderJobs(jobs) {
      jobsEl.textContent = "";
      if (!jobs || !jobs.length) {
        var empty = document.createElement("p");
        empty.className = "printer-jobs-empty";
        empty.textContent = "Nothing has been printed to this session yet.";
        jobsEl.appendChild(empty);
        return;
      }
      var list = document.createElement("ul");
      list.className = "printer-job-list";
      for (var i = 0; i < jobs.length; i++) {
        list.appendChild(jobRow(jobs[i]));
      }
      jobsEl.appendChild(list);
    }

    function jobRow(job) {
      var item = document.createElement("li");
      item.className = "printer-job";

      var meta = document.createElement("div");
      meta.className = "printer-job-meta";
      var when = document.createElement("span");
      when.className = "printer-job-when";
      when.textContent = timeLabel(job.received_at) || job.name;
      meta.appendChild(when);
      var size = document.createElement("span");
      size.className = "printer-job-size";
      size.textContent = bytesLabel(job.bytes);
      meta.appendChild(size);
      if (job.truncated) {
        // Said out loud rather than left to the file name. A report that
        // stopped two-thirds of the way down looks complete until somebody
        // reaches the end of it.
        var flag = document.createElement("span");
        flag.className = "printer-job-truncated";
        flag.textContent = "truncated";
        meta.appendChild(flag);
      }
      item.appendChild(meta);

      var actions = document.createElement("div");
      actions.className = "printer-job-actions";
      var download = document.createElement("a");
      download.className = "printer-job-download";
      download.href = "/printer/jobs/download?name=" + encodeURIComponent(job.name);
      download.setAttribute("download", job.name);
      download.textContent = "Download";
      actions.appendChild(download);
      var remove = document.createElement("button");
      remove.type = "button";
      remove.className = "printer-job-delete";
      remove.textContent = "Delete";
      remove.addEventListener("click", function () {
        request("POST", "/printer/jobs/delete?name=" + encodeURIComponent(job.name))
          .then(function () {
            lastFailure = "";
            return load();
          })
          .catch(function (err) {
            lastFailure = err.message;
            return load();
          });
      });
      actions.appendChild(remove);
      item.appendChild(actions);
      return item;
    }

    function render(state) {
      var running = !!state.running;
      var available = !!state.available;
      if (running) {
        // Whatever went wrong before, it is not what is happening now.
        lastFailure = "";
      }
      if (lastFailure) {
        setStatus(lastFailure, true);
      } else {
        setStatus(describe(state), available && state.printer && !running && state.printer.error);
      }
      startBtn.hidden = running;
      startBtn.disabled = !available || busy;
      stopBtn.hidden = !running;
      stopBtn.disabled = busy;
      fieldsEl.disabled = running || !available;
      renderJobs(state.jobs);
    }

    function load() {
      // A poll must not fight the request the operator is waiting on.
      if (busy) {
        return Promise.resolve();
      }
      return request("GET", "/printer/status")
        .then(render)
        .catch(function (err) {
          setStatus(err.message, true);
        });
    }

    formEl.addEventListener("submit", function (event) {
      event.preventDefault();
      if (busy) {
        return;
      }
      busy = true;
      startBtn.disabled = true;
      lastFailure = "";
      setStatus("Binding the printer LU…", false);
      request("POST", "/printer/start", startBody())
        .then(function () {
          busy = false;
          return load();
        })
        .catch(function (err) {
          busy = false;
          startBtn.disabled = false;
          lastFailure = err.message;
          return load();
        });
    });

    stopBtn.addEventListener("click", function () {
      if (busy) {
        return;
      }
      busy = true;
      stopBtn.disabled = true;
      lastFailure = "";
      request("POST", "/printer/stop")
        .then(function () {
          busy = false;
          return load();
        })
        .catch(function (err) {
          busy = false;
          stopBtn.disabled = false;
          lastFailure = err.message;
          return load();
        });
    });

    // Refresh is the operator saying "tell me where things stand now", which
    // is also the one action that should clear a held failure.
    refreshBtn.addEventListener("click", function () {
      lastFailure = "";
      load();
    });

    for (var i = 0; i < bindRadios.length; i++) {
      bindRadios[i].addEventListener("change", syncBindFields);
    }
    syncBindFields();

    // Focus, the Tab trap, the background scroll lock and the focus restore
    // all belong to pushModal/popModal — see modal-utils.js.
    function closeModal() {
      modal.hidden = true;
      if (timer) {
        window.clearInterval(timer);
        timer = null;
      }
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
    }

    function openModal() {
      modal.hidden = false;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
        window.ThreeSeventyWeb.pushModal(modal, closeModal);
      }
      lastFailure = "";
      load();
      if (!timer) {
        timer = window.setInterval(load, POLL_MS);
      }
    }

    var openButtons = document.querySelectorAll("[data-printer-open]");
    for (var o = 0; o < openButtons.length; o++) {
      openButtons[o].addEventListener("click", openModal);
    }
    var closeButtons = modal.querySelectorAll("[data-printer-close]");
    for (var k = 0; k < closeButtons.length; k++) {
      closeButtons[k].addEventListener("click", closeModal);
    }
    modal.addEventListener("click", function (event) {
      if (event.target === modal) {
        closeModal();
      }
    });
  });
})();
