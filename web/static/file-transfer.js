// IND$FILE file transfer UI.
//
// The most-cited gap against Quick3270, PCOMM and Rumba+: getting a dataset
// off the host into a spreadsheet, or a file up to it, without leaving the
// terminal.
//
// The form is deliberately two-tier. Send/receive, host file and text-vs-
// binary are what almost every transfer needs; record format and space
// allocation matter only when creating a new TSO dataset, so they sit behind
// a disclosure rather than confronting everyone with eight fields.
(function () {
  "use strict";

  var modal = null;
  var formEl = null;
  var statusEl = null;
  var busy = false;

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type, options);
    }
  }

  function setStatus(message, kind) {
    if (!statusEl) {
      return;
    }
    statusEl.textContent = message || "";
    statusEl.classList.toggle("is-error", kind === "error");
    statusEl.classList.toggle("is-busy", kind === "busy");
  }

  function direction() {
    var checked = formEl.querySelector('input[name="direction"]:checked');
    return checked ? checked.value : "receive";
  }

  // Sending needs a file and can create a dataset; receiving needs neither.
  function syncDirection() {
    var sending = direction() === "send";
    modal.setAttribute("data-transfer-direction", sending ? "send" : "receive");
    formEl.elements.file.required = sending;
    var submit = modal.querySelector("[data-transfer-submit]");
    if (submit) {
      submit.textContent = sending ? "Send to host" : "Receive from host";
    }
  }

  function collect() {
    var data = new FormData();
    data.append("hostFile", formEl.elements.hostFile.value);
    data.append("hostType", formEl.elements.hostType.value);
    data.append("mode", formEl.elements.mode.value);
    if (formEl.elements.mode.value === "ascii") {
      data.append("cr", formEl.elements.cr.value);
    }
    if (direction() === "send") {
      data.append("exist", formEl.elements.exist.value);
      data.append("recfm", formEl.elements.recfm.value);
      data.append("lrecl", formEl.elements.lrecl.value || "0");
      data.append("blksize", formEl.elements.blksize.value || "0");
      data.append("allocation", formEl.elements.allocation.value);
      data.append("primarySpace", formEl.elements.primarySpace.value || "0");
      data.append("secondarySpace", formEl.elements.secondarySpace.value || "0");
    }
    return data;
  }

  function send(event) {
    event.preventDefault();
    if (busy) {
      return;
    }
    var file = formEl.elements.file.files && formEl.elements.file.files[0];
    if (!file) {
      setStatus("Choose a file to send.", "error");
      return;
    }
    var data = collect();
    data.append("file", file);

    busy = true;
    // A transfer runs at 3270 data-stream speed, which is slow enough that
    // saying nothing would look like a hang.
    setStatus("Sending " + file.name + " — this can take a while…", "busy");
    fetch("/transfer/send", { method: "POST", credentials: "same-origin", body: data })
      .then(function (response) {
        return response.json().catch(function () { return {}; }).then(function (payload) {
          if (!response.ok) {
            throw new Error(payload.error || "transfer failed");
          }
          return payload;
        });
      })
      .then(
        function (payload) {
          busy = false;
          setStatus("Sent " + file.name + " (" + payload.bytes + " bytes).");
          notify("File sent to the host.", "success");
        },
        function (err) {
          busy = false;
          setStatus((err && err.message) || "The transfer failed.", "error");
        }
      );
  }

  function receive(event) {
    event.preventDefault();
    if (busy) {
      return;
    }
    var hostFile = formEl.elements.hostFile.value.trim();
    if (!hostFile) {
      setStatus("Enter the host file to receive.", "error");
      return;
    }

    busy = true;
    setStatus("Receiving " + hostFile + " — this can take a while…", "busy");
    fetch("/transfer/receive", { method: "POST", credentials: "same-origin", body: collect() })
      .then(function (response) {
        if (!response.ok) {
          return response.json().catch(function () { return {}; }).then(function (payload) {
            throw new Error(payload.error || "transfer failed");
          });
        }
        // The server names the download from the host file; honour it.
        var name = hostFile.replace(/[()]/g, ".").replace(/\.+$/, "") || "download.txt";
        var header = response.headers.get("Content-Disposition") || "";
        var match = header.match(/filename="([^"]+)"/);
        if (match) {
          name = match[1];
        }
        return response.blob().then(function (blob) {
          return { blob: blob, name: name };
        });
      })
      .then(
        function (result) {
          busy = false;
          var url = URL.createObjectURL(result.blob);
          var link = document.createElement("a");
          link.href = url;
          link.download = result.name;
          document.body.appendChild(link);
          link.click();
          document.body.removeChild(link);
          // Revoking immediately can cancel the download in some browsers.
          window.setTimeout(function () {
            URL.revokeObjectURL(url);
          }, 30000);
          setStatus("Received " + result.name + " (" + result.blob.size + " bytes).");
          notify("File received from the host.", "success");
        },
        function (err) {
          busy = false;
          setStatus((err && err.message) || "The transfer failed.", "error");
        }
      );
  }

  function submit(event) {
    if (direction() === "send") {
      send(event);
    } else {
      receive(event);
    }
  }

  function open() {
    if (!modal) {
      return;
    }
    modal.hidden = false;
    setStatus("");
    syncDirection();
    formEl.elements.hostFile.focus();
  }

  function close() {
    if (modal) {
      modal.hidden = true;
    }
  }

  function isOpen() {
    return !!modal && !modal.hidden;
  }

  function build() {
    modal = document.createElement("div");
    modal.className = "transfer-modal";
    modal.hidden = true;
    modal.setAttribute("data-transfer-modal", "");
    modal.innerHTML = [
      '<div class="transfer-modal-backdrop" data-transfer-close></div>',
      '<div class="transfer-modal-content" role="dialog" aria-modal="true" aria-labelledby="transfer-title">',
      '  <div class="transfer-modal-header">',
      '    <h3 id="transfer-title">File transfer <span class="subtle">IND$FILE</span></h3>',
      '    <button type="button" data-transfer-close>Close</button>',
      "  </div>",
      '  <form data-transfer-form>',
      '    <div class="transfer-direction" role="radiogroup" aria-label="Transfer direction">',
      '      <label><input type="radio" name="direction" value="receive" checked> Receive from host</label>',
      '      <label><input type="radio" name="direction" value="send"> Send to host</label>',
      "    </div>",
      '    <label>Host file<input name="hostFile" type="text" required maxlength="128" placeholder="USER.DATA(MEMBER)"></label>',
      '    <div class="transfer-send-only"><label>Local file<input name="file" type="file"></label></div>',
      '    <div class="transfer-grid">',
      '      <label>Host<select name="hostType"><option value="tso">TSO</option><option value="vm">VM</option><option value="cics">CICS</option></select></label>',
      '      <label>Mode<select name="mode"><option value="ascii">Text (ASCII)</option><option value="binary">Binary</option></select></label>',
      '      <label class="transfer-ascii-only">Line endings<select name="cr"><option value="remove">Remove CR</option><option value="add">Add CR</option><option value="keep">Keep</option></select></label>',
      '      <label class="transfer-send-only">If it exists<select name="exist"><option value="replace">Replace</option><option value="keep">Keep (fail)</option><option value="append">Append</option></select></label>',
      "    </div>",
      '    <details class="transfer-advanced transfer-send-only">',
      "      <summary>New dataset options</summary>",
      '      <p class="subtle">Only needed when the host file does not exist yet.</p>',
      '      <div class="transfer-grid">',
      '        <label>Record format<select name="recfm"><option value="">Host default</option><option value="fixed">Fixed</option><option value="variable">Variable</option><option value="undefined">Undefined</option></select></label>',
      '        <label>Record length<input name="lrecl" type="number" min="0" max="32760" placeholder="80"></label>',
      '        <label>Block size<input name="blksize" type="number" min="0" max="32760"></label>',
      '        <label>Allocation<select name="allocation"><option value="">Host default</option><option value="tracks">Tracks</option><option value="cylinders">Cylinders</option><option value="avblock">Average blocks</option></select></label>',
      '        <label>Primary space<input name="primarySpace" type="number" min="0"></label>',
      '        <label>Secondary space<input name="secondarySpace" type="number" min="0"></label>',
      "      </div>",
      "    </details>",
      '    <div class="transfer-actions">',
      '      <span class="transfer-status" data-transfer-status aria-live="polite"></span>',
      '      <button type="submit" data-transfer-submit>Receive from host</button>',
      "    </div>",
      '    <p class="subtle transfer-note">The host session must be at a command prompt that can run IND$FILE — a transfer started from inside an application screen will fail.</p>',
      "  </form>",
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    formEl = modal.querySelector("[data-transfer-form]");
    statusEl = modal.querySelector("[data-transfer-status]");

    formEl.addEventListener("submit", submit);
    var radios = formEl.querySelectorAll('input[name="direction"]');
    for (var i = 0; i < radios.length; i++) {
      radios[i].addEventListener("change", syncDirection);
    }
    formEl.elements.mode.addEventListener("change", function () {
      modal.setAttribute("data-transfer-mode", formEl.elements.mode.value);
    });
    modal.setAttribute("data-transfer-mode", "ascii");

    var closers = modal.querySelectorAll("[data-transfer-close]");
    for (var j = 0; j < closers.length; j++) {
      closers[j].addEventListener("click", close);
    }
    document.addEventListener(
      "keydown",
      function (event) {
        if (isOpen() && event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          close();
        }
      },
      true
    );
  }

  document.addEventListener("DOMContentLoaded", function () {
    if (!document.querySelector(".terminal-shell")) {
      return;
    }
    build();
    var trigger = document.querySelector("[data-transfer-open]");
    if (trigger) {
      trigger.addEventListener("click", open);
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.fileTransfer = { open: open, close: close, isOpen: isOpen };
})();
