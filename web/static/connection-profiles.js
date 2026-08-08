// Connection profiles UI.
//
// TLS, LU name, terminal model and code page used to be server-wide
// environment variables, so every session in a deployment shared them and the
// connect form took "hostname:port" and nothing else. Profiles make those
// per-host, which is what lets one host run on TLS while another runs in the
// clear, or a model 2 against one application and a model 4 against another.
//
// One modal serves two jobs, because they are the same job with a different
// ending: "pick a profile and connect" and "manage the list". Splitting them
// into two dialogs would duplicate the whole form for no gain.
(function () {
  "use strict";

  var profiles = [];
  var modal = null;
  var listEl = null;
  var formEl = null;
  var statusEl = null;
  var mode = "manage"; // or "pick"
  var onPick = null;

  function notify(message, type, options) {
    if (window.ThreeSeventyWeb && typeof window.ThreeSeventyWeb.notify === "function") {
      window.ThreeSeventyWeb.notify(message, type, options);
    }
  }

  function api(path, options) {
    return fetch(path, Object.assign({ credentials: "same-origin" }, options || {})).then(function (response) {
      return response
        .json()
        .catch(function () {
          return {};
        })
        .then(function (payload) {
          if (!response.ok) {
            throw new Error(payload.error || "request failed");
          }
          return payload;
        });
    });
  }

  function load() {
    return api("/api/profiles").then(function (payload) {
      profiles = payload && Array.isArray(payload.profiles) ? payload.profiles : [];
      renderList();
      return profiles;
    });
  }

  function setStatus(message, isError) {
    if (!statusEl) {
      return;
    }
    statusEl.textContent = message || "";
    statusEl.classList.toggle("is-error", !!isError);
  }

  // summarise renders the connection in the same shape s3270 will see it, so
  // what the operator reads is what actually gets dialled.
  function summarise(p) {
    var target = "";
    if (p.tls) {
      target += p.skipVerify ? "L:Y:" : "L:";
    }
    if (p.luName) {
      target += p.luName + "@";
    }
    target += p.host + ":" + (p.port || 3270);
    var extras = [];
    if (p.model) extras.push(p.model);
    if (p.codePage) extras.push(p.codePage);
    return extras.length ? target + "  ·  " + extras.join(" · ") : target;
  }

  function renderList() {
    if (!listEl) {
      return;
    }
    listEl.innerHTML = "";
    if (!profiles.length) {
      var empty = document.createElement("p");
      empty.className = "subtle";
      empty.textContent = "No connection profiles yet. Fill in the form below to create one.";
      listEl.appendChild(empty);
      return;
    }

    for (var i = 0; i < profiles.length; i++) {
      var p = profiles[i];
      var row = document.createElement("div");
      row.className = "profile-row";

      var main = document.createElement("button");
      main.type = "button";
      main.className = "profile-row-main";
      main.innerHTML =
        '<span class="profile-row-name"></span>' +
        '<span class="profile-row-target"></span>' +
        '<span class="profile-row-desc subtle"></span>';
      main.querySelector(".profile-row-name").textContent = p.name;
      main.querySelector(".profile-row-target").textContent = summarise(p);
      main.querySelector(".profile-row-desc").textContent = p.description || "";
      main.setAttribute(
        "aria-label",
        (mode === "pick" ? "Connect using " : "Edit ") + p.name + " — " + summarise(p)
      );
      (function (profile) {
        main.addEventListener("click", function () {
          if (mode === "pick") {
            choose(profile);
          } else {
            fill(profile);
          }
        });
      })(p);
      row.appendChild(main);

      if (p.tls) {
        var lock = document.createElement("span");
        lock.className = "profile-row-tls" + (p.skipVerify ? " is-unverified" : "");
        lock.textContent = p.skipVerify ? "TLS!" : "TLS";
        // "Encrypted but unverified" is materially weaker than TLS and is
        // worth saying out loud rather than showing the same padlock.
        lock.title = p.skipVerify
          ? "TLS with certificate verification disabled"
          : "TLS with certificate verification";
        row.appendChild(lock);
      }

      var edit = document.createElement("button");
      edit.type = "button";
      edit.className = "profile-row-action";
      edit.textContent = "Edit";
      edit.setAttribute("aria-label", "Edit profile " + p.name);
      (function (profile) {
        edit.addEventListener("click", function (event) {
          event.stopPropagation();
          fill(profile);
        });
      })(p);
      row.appendChild(edit);

      var del = document.createElement("button");
      del.type = "button";
      del.className = "profile-row-action danger";
      del.textContent = "Delete";
      del.setAttribute("aria-label", "Delete profile " + p.name);
      (function (profile) {
        del.addEventListener("click", function (event) {
          event.stopPropagation();
          remove(profile);
        });
      })(p);
      row.appendChild(del);

      listEl.appendChild(row);
    }
  }

  function fill(p) {
    if (!formEl) {
      return;
    }
    formEl.elements.name.value = p.name || "";
    formEl.elements.host.value = p.host || "";
    formEl.elements.port.value = p.port || 3270;
    formEl.elements.tls.checked = !!p.tls;
    formEl.elements.skipVerify.checked = !!p.skipVerify;
    formEl.elements.luName.value = p.luName || "";
    formEl.elements.model.value = p.model || "";
    formEl.elements.codePage.value = p.codePage || "";
    formEl.elements.description.value = p.description || "";
    syncSkipVerify();
    setStatus("Editing “" + p.name + "”. Saving replaces it.");
    formEl.elements.name.focus();
  }

  function clearForm() {
    if (!formEl) {
      return;
    }
    formEl.reset();
    formEl.elements.port.value = 3270;
    syncSkipVerify();
    setStatus("");
  }

  // Skip-verify is meaningless without TLS, and the server rejects the
  // combination — so the control follows suit rather than letting someone
  // tick a box that cannot be saved.
  function syncSkipVerify() {
    if (!formEl) {
      return;
    }
    var tls = formEl.elements.tls.checked;
    formEl.elements.skipVerify.disabled = !tls;
    if (!tls) {
      formEl.elements.skipVerify.checked = false;
    }
  }

  function save(event) {
    event.preventDefault();
    var payload = {
      name: formEl.elements.name.value,
      host: formEl.elements.host.value,
      port: parseInt(formEl.elements.port.value, 10) || 3270,
      tls: formEl.elements.tls.checked,
      skipVerify: formEl.elements.skipVerify.checked,
      luName: formEl.elements.luName.value,
      model: formEl.elements.model.value,
      codePage: formEl.elements.codePage.value,
      description: formEl.elements.description.value
    };
    api("/api/profiles", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload)
    }).then(
      function (result) {
        profiles = result.profiles || [];
        renderList();
        clearForm();
        setStatus("Saved “" + payload.name + "”.");
      },
      function (err) {
        setStatus((err && err.message) || "Could not save the profile.", true);
      }
    );
  }

  function remove(p) {
    if (!window.confirm("Delete the connection profile “" + p.name + "”?")) {
      return;
    }
    api("/api/profiles/delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: p.name })
    }).then(
      function (result) {
        profiles = result.profiles || [];
        renderList();
        setStatus("Deleted “" + p.name + "”.");
      },
      function (err) {
        setStatus((err && err.message) || "Could not delete the profile.", true);
      }
    );
  }

  function choose(p) {
    close();
    if (typeof onPick === "function") {
      onPick(p);
    }
  }

  function open(nextMode, picker) {
    mode = nextMode === "pick" ? "pick" : "manage";
    onPick = picker || null;
    if (!modal) {
      return;
    }
    modal.hidden = false;
    modal.setAttribute("data-profile-mode", mode);
    var title = modal.querySelector("[data-profiles-title]");
    if (title) {
      title.textContent = mode === "pick" ? "Open a session" : "Connection profiles";
    }
    clearForm();
    load().catch(function (err) {
      setStatus((err && err.message) || "Could not load profiles.", true);
    });
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
    modal.className = "profiles-modal";
    modal.hidden = true;
    modal.setAttribute("data-profiles-modal", "");
    modal.innerHTML = [
      '<div class="profiles-modal-backdrop" data-profiles-close></div>',
      '<div class="profiles-modal-content" role="dialog" aria-modal="true" aria-labelledby="profiles-title">',
      '  <div class="profiles-modal-header">',
      '    <h3 id="profiles-title" data-profiles-title>Connection profiles</h3>',
      '    <button type="button" data-profiles-close>Close</button>',
      "  </div>",
      '  <div class="profiles-list" data-profiles-list></div>',
      '  <form class="profiles-form" data-profiles-form>',
      '    <div class="profiles-form-grid">',
      '      <label>Name<input name="name" type="text" required maxlength="64" placeholder="CICS Production"></label>',
      '      <label>Host<input name="host" type="text" required placeholder="mainframe.example.com"></label>',
      '      <label>Port<input name="port" type="number" min="1" max="65535" value="3270"></label>',
      '      <label>LU name <span class="subtle">optional</span><input name="luName" type="text" placeholder="TCP00042"></label>',
      '      <label>Model <span class="subtle">optional</span><input name="model" type="text" placeholder="3279-4-E"></label>',
      '      <label>Code page <span class="subtle">optional</span><input name="codePage" type="text" placeholder="cp037"></label>',
      "    </div>",
      '    <label class="profiles-check"><input name="tls" type="checkbox"> Use TLS</label>',
      '    <label class="profiles-check"><input name="skipVerify" type="checkbox" disabled> Skip certificate verification <span class="subtle">weaker — only for self-signed test hosts</span></label>',
      '    <label>Description <span class="subtle">optional</span><input name="description" type="text" maxlength="200"></label>',
      '    <div class="profiles-form-actions">',
      '      <span class="profiles-status" data-profiles-status aria-live="polite"></span>',
      '      <button type="button" data-profiles-clear>Clear</button>',
      '      <button type="submit">Save profile</button>',
      "    </div>",
      "  </form>",
      "</div>"
    ].join("");
    document.body.appendChild(modal);

    listEl = modal.querySelector("[data-profiles-list]");
    formEl = modal.querySelector("[data-profiles-form]");
    statusEl = modal.querySelector("[data-profiles-status]");

    formEl.addEventListener("submit", save);
    formEl.elements.tls.addEventListener("change", syncSkipVerify);
    modal.querySelector("[data-profiles-clear]").addEventListener("click", clearForm);
    var closers = modal.querySelectorAll("[data-profiles-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
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
    build();

    var manage = document.querySelectorAll("[data-profiles-open]");
    for (var i = 0; i < manage.length; i++) {
      manage[i].addEventListener("click", function () {
        open("manage", null);
      });
    }

    // On the connect page, choosing a profile submits the connect form with
    // the profile name so the server applies its TLS/LU/model settings.
    var connectForm = document.getElementById("connect-form");
    var pickTrigger = document.querySelector("[data-profiles-connect]");
    if (pickTrigger && connectForm) {
      pickTrigger.addEventListener("click", function () {
        open("pick", function (profile) {
          var field = connectForm.querySelector('input[name="profile"]');
          if (!field) {
            field = document.createElement("input");
            field.type = "hidden";
            field.name = "profile";
            connectForm.appendChild(field);
          }
          field.value = profile.name;
          // The hostname input is required, so give it the profile's target
          // to satisfy validation; the server prefers the profile name.
          var hostInput = connectForm.querySelector('input[name="hostname"]');
          if (hostInput) {
            hostInput.value = profile.host + ":" + (profile.port || 3270);
          }
          connectForm.submit();
        });
      });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.connectionProfiles = {
    open: open,
    close: close,
    isOpen: isOpen,
    load: load,
    list: function () {
      return profiles.slice();
    },
    // Used by the session tab bar's "+" so opening another session offers
    // the same profiles rather than a bare hostname prompt.
    pick: function (callback) {
      open("pick", callback);
    }
  };
})();
