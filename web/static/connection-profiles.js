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
  var samplesEl = null;
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

  var canShare = false;
  var knownGroups = [];
  // The bundled sample apps a profile may point at instead of a mainframe.
  var sampleApps = [];

  function load() {
    return api("/api/profiles").then(function (payload) {
      canShare = !!payload.canShare;
      knownGroups = payload.groups || [];
      sampleApps = payload.sampleApps || [];
      syncShareVisibility();
      fillSampleApps();
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
    // Except for a bundled sample app, which is named the way it was chosen:
    // "sampleapp:petstore:3273" is the address, not something anybody picked.
    var sample = sampleAppFor(p.host);
    if (sample) {
      target = sample.name + " · port " + (p.port || sample.port);
    } else {
      if (p.tls) {
        target += p.skipVerify ? "L:Y:" : "L:";
      }
      if (p.luName) {
        target += p.luName + "@";
      }
      target += p.host + ":" + (p.port || 3270);
    }
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
      empty.textContent = sampleApps.length
        ? "No connection profiles yet. Pick a bundled sample app below, or fill in the form to create one."
        : "No connection profiles yet. Fill in the form below to create one.";
      listEl.appendChild(empty);
      renderSamples();
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
    renderSamples();
  }

  // renderSamples offers the bundled sample apps as things to connect to, not
  // just as values to copy into a profile.
  //
  // It is only drawn when the modal is picking a session to open. Opening one
  // used to mean either having a profile already or typing an address into a
  // browser prompt, and the address of a sample app — "sampleapp:<id>" on a
  // port that is not 3270 — is not something anybody is told. An instance with
  // no mainframe to reach is exactly the instance where the second session is
  // hardest to open, which is the wrong way round.
  function renderSamples() {
    if (!samplesEl) {
      return;
    }
    var wrap = modal.querySelector("[data-profiles-samples]");
    if (!wrap) {
      return;
    }
    wrap.hidden = mode !== "pick" || !sampleApps.length;
    samplesEl.innerHTML = "";
    if (wrap.hidden) {
      return;
    }
    sampleApps.forEach(function (app) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "profile-row-main profile-sample-row";
      button.innerHTML =
        '<span class="profile-row-name"></span>' +
        '<span class="profile-row-target"></span>';
      button.querySelector(".profile-row-name").textContent = app.name;
      button.querySelector(".profile-row-target").textContent = "port " + app.port;
      button.setAttribute("aria-label", "Open a session to the " + app.name + " sample app");
      button.addEventListener("click", function () {
        chooseTarget({
          name: "",
          target: app.host + ":" + app.port,
          label: app.name
        });
      });
      samplesEl.appendChild(button);
    });
  }

  // syncShareVisibility shows the publishing block only where publishing means
  // something, and fills the group list from the names already in use so the
  // same team is not spelled two ways.
  function syncShareVisibility() {
    if (!formEl) {
      return;
    }
    var share = formEl.querySelector("[data-profiles-share]");
    if (share) {
      share.hidden = !canShare;
    }
    var list = formEl.querySelector("[data-profiles-known-groups]");
    if (list) {
      list.textContent = "";
      knownGroups.forEach(function (name) {
        var option = document.createElement("option");
        option.value = name;
        list.appendChild(option);
      });
    }
  }

  /* ------------------------------------------------------------ sample apps
     A profile can point at one of the bundled sample apps just as well as at
     a mainframe, and on an instance being evaluated or taught on there is
     nothing else to point it at. Writing one by hand means knowing that they
     are addressed as "sampleapp:<id>" and that they do not listen on 3270 —
     two things nobody is told — so they are offered as a list instead, and
     choosing one fills the connection in. */

  function fillSampleApps() {
    if (!formEl) {
      return;
    }
    var field = formEl.querySelector("[data-profiles-sample-field]");
    var select = formEl.elements.sampleApp;
    if (!field || !select) {
      return;
    }
    field.hidden = !sampleApps.length;
    while (select.options.length > 1) {
      select.remove(1);
    }
    sampleApps.forEach(function (app) {
      var option = document.createElement("option");
      option.value = app.host;
      option.textContent = app.name;
      select.appendChild(option);
    });
    syncSampleSelect();
  }

  function sampleAppFor(hostValue) {
    var host = String(hostValue || "").trim().toLowerCase();
    for (var i = 0; i < sampleApps.length; i++) {
      if (String(sampleApps[i].host).toLowerCase() === host) {
        return sampleApps[i];
      }
    }
    return null;
  }

  // The select follows the host field rather than the other way round, so
  // typing over a filled-in host puts it back to "a mainframe on the network"
  // instead of claiming a sample app the profile no longer names.
  function syncSampleSelect() {
    if (!formEl || !formEl.elements.sampleApp) {
      return;
    }
    var chosen = sampleAppFor(formEl.elements.host.value);
    formEl.elements.sampleApp.value = chosen ? chosen.host : "";
  }

  function applySampleApp() {
    if (!formEl) {
      return;
    }
    var chosen = sampleAppFor(formEl.elements.sampleApp.value);
    if (!chosen) {
      return;
    }
    formEl.elements.host.value = chosen.host;
    formEl.elements.port.value = chosen.port;
    // A sample app is a plaintext listener on loopback that this process
    // started. TLS against it cannot succeed, so a tick left behind from a
    // previous edit would only produce a profile that fails to connect.
    formEl.elements.tls.checked = false;
    formEl.elements.skipVerify.checked = false;
    syncSkipVerify();
    if (!formEl.elements.name.value.trim()) {
      formEl.elements.name.value = chosen.name;
    }
    setStatus("Filled in " + chosen.name + " on port " + chosen.port + ".");
  }

  function commaList(value) {
    return String(value || "").split(",").map(function (s) {
      return s.trim();
    }).filter(function (s) {
      return s !== "";
    });
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
    if (formEl.elements.publish) {
      formEl.elements.publish.checked = !!p.shared;
      formEl.elements.audienceGroups.value = (p.groups || []).join(", ");
      formEl.elements.audienceUsers.value = (p.users || []).join(", ");
      formEl.elements.audienceRoles.value = (p.roles || [])[0] || "";
    }
    syncSkipVerify();
    // After the host, so the picker agrees with the profile actually loaded.
    syncSampleSelect();
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
    syncSampleSelect();
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
    if (canShare && formEl.elements.publish) {
      payload.publish = formEl.elements.publish.checked;
      payload.groups = commaList(formEl.elements.audienceGroups.value);
      payload.users = commaList(formEl.elements.audienceUsers.value);
      var role = formEl.elements.audienceRoles.value;
      payload.roles = role ? [role] : [];
    }
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

  // What a pick hands back. A profile is named, and the name is what the
  // server should be given — it carries TLS, LU, model and code page, none of
  // which "host:port" can express. A sample app or a typed address has no
  // profile behind it, so it hands back the target alone and leaves name
  // empty; every caller has to cope with both, because both are ordinary.
  function chooseTarget(choice) {
    close();
    if (typeof onPick === "function") {
      onPick(choice);
    }
  }

  function choose(p) {
    chooseTarget({
      name: p.name,
      target: p.host + ":" + (p.port || 3270),
      label: p.name
    });
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
    var direct = modal.querySelector("[data-profiles-direct]");
    if (direct) {
      direct.hidden = mode !== "pick";
      direct.reset();
    }
    renderSamples();
    clearForm();
    // Escape, focus, the Tab trap, the background scroll lock and the focus
    // restore all belong to pushModal/popModal — see modal-utils.js.
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      window.ThreeSeventyWeb.pushModal(modal, close);
    }
    load().catch(function (err) {
      setStatus((err && err.message) || "Could not load profiles.", true);
    });
  }

  function close() {
    if (modal) {
      modal.hidden = true;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
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
      // Both of these are for picking a session to open, and are hidden while
      // the modal is being used to manage the list.
      '  <div class="profiles-samples" data-profiles-samples hidden>',
      '    <h4>Bundled sample apps</h4>',
      '    <p class="subtle">TN3270 servers 3270Web starts itself. Pick one to open a session against it — no mainframe needed.</p>',
      '    <div class="profiles-sample-list" data-profiles-sample-list></div>',
      "  </div>",
      // The typed-address path. It lives in the dialog rather than in a
      // window.prompt because a prompt cannot be styled, cannot say what a
      // valid address looks like, and does not open at all inside a sandboxed
      // frame — which is where an embedded terminal runs.
      '  <form class="profiles-direct" data-profiles-direct hidden>',
      '    <div class="profiles-direct-row">',
      '      <label>Or connect to a host<input name="target" type="text" placeholder="hostname:port" autocomplete="off" autocapitalize="none" spellcheck="false"></label>',
      '      <button type="submit">Connect</button>',
      "    </div>",
      "  </form>",
      '  <form class="profiles-form" data-profiles-form>',
      // Hidden on an instance with no bundled samples, which is why it is a
      // wrapper rather than the label itself: .profiles-form label is
      // display:flex, and [hidden] loses to it.
      '    <div class="profiles-sample" data-profiles-sample-field hidden>',
      // No "optional" tag: the default option says so in words, and the tag
      // would wrap onto its own line above the select.
      '      <label>Bundled sample app',
      '        <select name="sampleApp" aria-describedby="profiles-sample-hint">',
      '          <option value="">A mainframe on the network</option>',
      "        </select>",
      "      </label>",
      '      <p class="subtle profiles-sample-hint" id="profiles-sample-hint">Choosing one fills in the host and port below. The sample apps are TN3270 servers 3270Web starts itself, so a profile naming one connects with no mainframe to reach.</p>',
      "    </div>",
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
      // Publishing and its audience are one block, shown only to an
      // administrator: on an instance with one operator there is nobody to
      // publish to, and for anybody else a save is their own copy.
      '    <fieldset class="profiles-share" data-profiles-share hidden>',
      '      <legend>Who this host is for</legend>',
      '      <label class="profiles-check"><input name="publish" type="checkbox"> Share with everyone <span class="subtle">otherwise this is your own copy</span></label>',
      '      <div class="profiles-form-grid" data-profiles-audience>',
      '        <label>Groups <span class="subtle">comma separated</span><input name="audienceGroups" type="text" list="profile-known-groups" placeholder="payments, ops"></label>',
      '        <label>Users <span class="subtle">comma separated</span><input name="audienceUsers" type="text" placeholder="alice, bob"></label>',
      '        <label>Roles<select name="audienceRoles"><option value="">Any role</option><option value="user">Users only</option><option value="admin">Administrators only</option></select></label>',
      '      </div>',
      '      <datalist id="profile-known-groups" data-profiles-known-groups></datalist>',
      '      <p class="subtle profiles-audience-hint">Leave all three empty and everyone sees this host. Naming any of them narrows it to whoever matches at least one.</p>',
      "    </fieldset>",
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
    samplesEl = modal.querySelector("[data-profiles-sample-list]");

    var directForm = modal.querySelector("[data-profiles-direct]");
    if (directForm) {
      directForm.addEventListener("submit", function (event) {
        event.preventDefault();
        var typed = directForm.elements.target.value.trim();
        if (!typed) {
          directForm.elements.target.focus();
          return;
        }
        chooseTarget({ name: "", target: typed, label: typed });
      });
    }

    syncShareVisibility();
    fillSampleApps();
    formEl.addEventListener("submit", save);
    formEl.elements.tls.addEventListener("change", syncSkipVerify);
    formEl.elements.sampleApp.addEventListener("change", applySampleApp);
    formEl.elements.host.addEventListener("input", syncSampleSelect);
    modal.querySelector("[data-profiles-clear]").addEventListener("click", clearForm);
    var closers = modal.querySelectorAll("[data-profiles-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }
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
        open("pick", function (choice) {
          var field = connectForm.querySelector('input[name="profile"]');
          if (!field) {
            field = document.createElement("input");
            field.type = "hidden";
            field.name = "profile";
            connectForm.appendChild(field);
          }
          // Empty for a sample app or a typed address: there is no profile to
          // apply, and a stale name left in the field would connect somewhere
          // else entirely.
          field.value = choice.name || "";
          // The hostname input is required, so give it the target to satisfy
          // validation; the server prefers the profile name when there is one.
          var hostInput = connectForm.querySelector('input[name="hostname"]');
          if (hostInput) {
            hostInput.value = choice.target;
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
