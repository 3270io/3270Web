// Macro import: a macro file written for an installed terminal emulator,
// translated into a recording this terminal can replay.
//
// The report is the feature. A shop moving off a desktop client has years of
// macros, and the question that decides whether it can move is not "did this
// one convert" but "which lines did not, and what do I do about them". So the
// panel leads with the steps it produced, then lists every line it would not
// translate with the reason, and only then offers to use the result — with
// Debug named as the way to run it the first time, because a translated flow
// deserves to be stepped through before it types into a live host.
//
// The server has already loaded the recording into the session by the time
// this renders, so closing the panel reloads the page: the Automation menu
// has to come back showing the loaded recording rather than the import button
// that has just been used.
(function () {
  "use strict";

  document.addEventListener("DOMContentLoaded", function () {
    var modal = document.querySelector("[data-macro-import-modal]");
    var fileInput = document.querySelector("[data-macro-import-file]");
    if (!modal || !fileInput) {
      return;
    }

    var statusEl = modal.querySelector("[data-macro-import-status]");
    var summaryEl = modal.querySelector("[data-macro-import-summary]");
    var advisoriesEl = modal.querySelector("[data-macro-import-advisories]");
    var notesWrap = modal.querySelector("[data-macro-import-notes-wrap]");
    var notesEl = modal.querySelector("[data-macro-import-notes]");
    var loaded = false;

    function setStatus(text, isError) {
      if (!statusEl) {
        return;
      }
      statusEl.textContent = text || "";
      statusEl.hidden = !text;
      if (isError) {
        statusEl.setAttribute("data-error", "1");
      } else {
        statusEl.removeAttribute("data-error");
      }
    }

    function clear() {
      summaryEl.textContent = "";
      advisoriesEl.textContent = "";
      advisoriesEl.hidden = true;
      notesEl.textContent = "";
      notesWrap.hidden = true;
      setStatus("", false);
    }

    function plural(count, one, many) {
      return count === 1 ? one : many;
    }

    // renderReport shows the same account whether or not anything loaded: a
    // file that produced no steps still has a reason per line, and that is
    // the answer somebody needs.
    function renderReport(report, wasLoaded) {
      var notes = report.notes || [];
      var steps = report.stepTotal || 0;
      var lines = [];

      if (wasLoaded) {
        lines.push(
          "Loaded " +
            report.name +
            " — " +
            steps +
            " " +
            plural(steps, "step", "steps") +
            " from " +
            report.translated +
            " of " +
            report.statements +
            " " +
            plural(report.statements, "statement", "statements") +
            "."
        );
      } else {
        lines.push(
          "Nothing in this file became a step. " +
            report.statements +
            " " +
            plural(report.statements, "statement", "statements") +
            " were read."
        );
      }
      if (notes.length) {
        lines.push(
          notes.length +
            " " +
            plural(notes.length, "line needs", "lines need") +
            " a person: they are listed below and were left out of the recording."
        );
      } else if (wasLoaded) {
        lines.push("Every statement in the file translated.");
      }
      summaryEl.textContent = lines.join(" ");

      var advisories = report.advisories || [];
      advisoriesEl.textContent = "";
      if (advisories.length) {
        for (var a = 0; a < advisories.length; a++) {
          var item = document.createElement("li");
          item.textContent = advisories[a];
          advisoriesEl.appendChild(item);
        }
      }
      advisoriesEl.hidden = advisories.length === 0;

      notesEl.textContent = "";
      for (var i = 0; i < notes.length; i++) {
        var note = notes[i];
        var item = document.createElement("li");

        var head = document.createElement("div");
        head.className = "macro-import-note-head";
        var lineLabel = document.createElement("span");
        lineLabel.className = "macro-import-line";
        lineLabel.textContent = "Line " + note.line;
        head.appendChild(lineLabel);
        var code = document.createElement("code");
        code.textContent = note.source;
        head.appendChild(code);
        item.appendChild(head);

        var reason = document.createElement("p");
        reason.className = "macro-import-note-reason";
        reason.textContent = note.reason;
        item.appendChild(reason);

        notesEl.appendChild(item);
      }
      notesWrap.hidden = notes.length === 0;

      if (wasLoaded) {
        setStatus(
          "Step through it with Debug recording before you play it: a translated flow has never met this host.",
          false
        );
      }
    }

    function closeModal() {
      modal.hidden = true;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
      if (loaded) {
        // The session now holds a recording the menu does not know about yet.
        window.location.reload();
      }
    }

    function openModal() {
      modal.hidden = false;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
        window.ThreeSeventyWeb.pushModal(modal, closeModal);
      }
    }

    function importFile(file) {
      loaded = false;
      clear();
      openModal();
      setStatus("Translating " + file.name + "…", false);

      var body = new FormData();
      body.append("macro", file);
      fetch("/workflow/import-macro", {
        method: "POST",
        headers: { Accept: "application/json" },
        body: body
      })
        .then(function (res) {
          return res
            .json()
            .catch(function () {
              return {};
            })
            .then(function (payload) {
              return { ok: res.ok, status: res.status, payload: payload };
            });
        })
        .then(function (result) {
          if (result.ok) {
            loaded = true;
            renderReport(result.payload, true);
            return;
          }
          if (result.payload && result.payload.report) {
            renderReport(result.payload.report, false);
            setStatus(result.payload.error || "This file produced no steps.", true);
            return;
          }
          setStatus(
            (result.payload && result.payload.error) || "The macro file could not be imported.",
            true
          );
        })
        .catch(function () {
          setStatus("The macro file could not be imported.", true);
        });
    }

    var openButtons = document.querySelectorAll("[data-macro-import-open]");
    for (var i = 0; i < openButtons.length; i++) {
      openButtons[i].addEventListener("click", function () {
        fileInput.click();
      });
    }

    fileInput.addEventListener("change", function () {
      if (fileInput.files && fileInput.files.length > 0) {
        importFile(fileInput.files[0]);
        // Let the same file be picked again after a failed attempt.
        fileInput.value = "";
      }
    });

    var closeButtons = modal.querySelectorAll("[data-macro-import-close]");
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
