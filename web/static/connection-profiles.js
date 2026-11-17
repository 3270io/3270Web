// Connection profiles UI.
//
// TLS, LU name, terminal model and code page used to be server-wide
// environment variables, so every session in a deployment shared them and the
// connect form took "hostname:port" and nothing else. Profiles make those
// per-host, which is what lets one host run on TLS while another runs in the
// clear, or a model 2 against one application and a model 4 against another.
//
// Two dialogs, because there are two jobs and they are asked for at different
// moments:
//
//   Open a connection    start a session: a saved profile, a bundled sample
//                        app, or a host typed in. Nothing to fill in, so it is
//                        short, and it is what the Session menu, the connect
//                        page's button, "+ New session" and Alt+N all open.
//   Manage connections   keep the list: add, edit, delete, import, export, and
//                        the sample-app picker that fills a profile in. It is
//                        reached from the Session menu, from the Open dialog's
//                        "Manage connections…" and from the connect page's
//                        "Save this connection…".
//
// They used to be one dialog that changed its title and hid parts of itself
// depending on how it had been opened. A dialog with three names and a second
// sample-app list inside its own editor was neither quick to use nor easy to
// describe, so each now has one title, fixed in its markup.
(function () {
  "use strict";

  var profiles = [];
  var canShare = false;
  var knownGroups = [];
  // The bundled sample apps a profile may point at instead of a mainframe.
  var sampleApps = [];
  // Whether the list has arrived. Before it has, "no saved connections" would
  // be a claim about something not yet known, so the lists say they are
  // loading instead.
  var loaded = false;
  var loadFailed = false;

  // The Open dialog: what to start a session against.
  var openDlg = null;
  // What to do with the choice made in it. A profile hands back its name, a
  // sample app or a typed address its target alone — see chooseTarget.
  var onPick = null;

  // The Manage dialog: the editor and the list it edits.
  var manageDlg = null;
  var formEl = null;
  var statusEl = null;
  // Deleting a profile removes it for good — and, for a published one, out
  // from under everyone it was shared with — so it goes through the same
  // in-panel confirm dialog every other destructive action here uses rather
  // than window.confirm(), which is unbranded, blocks the tab, and (like
  // every native dialog) never appears at all inside the sandboxed frame an
  // embedded terminal runs in.
  var deleteConfirmEl = null;
  var deleteConfirmDesc = null;
  var deleteConfirmCancel = null;
  var deleteConfirmAccept = null;
  var pendingDelete = null;

  // What had the keyboard before either dialog opened, kept across the hand-off
  // from one to the other so closing the second still lands where the first
  // was started from.
  var openerEl = null;
  // Whether the menu item that started it was chosen from the keyboard.
  var openerKeyboard = false;

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

  // What the last successful load looked like, so a refresh that finds nothing
  // new does not redraw the lists. Redrawing replaces every button in them, and
  // a keyboard user who has already Tabbed onto one would find the focus
  // dropped out from under them.
  var lastLoad = "";

  function load() {
    return api("/api/profiles").then(
      function (payload) {
        var next = {
          canShare: !!payload.canShare,
          groups: payload.groups || [],
          sampleApps: payload.sampleApps || [],
          profiles: payload && Array.isArray(payload.profiles) ? payload.profiles : []
        };
        var signature = JSON.stringify(next);
        var changed = !loaded || loadFailed || signature !== lastLoad;
        lastLoad = signature;
        loaded = true;
        loadFailed = false;
        canShare = next.canShare;
        knownGroups = next.groups;
        sampleApps = next.sampleApps;
        profiles = next.profiles;
        if (changed) {
          syncShareVisibility();
          fillSampleApps();
          renderLists();
        }
        return profiles;
      },
      function (err) {
        loadFailed = true;
        renderLists();
        throw err;
      }
    );
  }

  // Manage's line of feedback, beside the Save button.
  function setStatus(message, isError) {
    if (!statusEl) {
      return;
    }
    statusEl.textContent = message || "";
    statusEl.classList.toggle("is-error", !!isError);
  }

  // Open's, under the saved list. It only ever has a failure to report.
  function setOpenStatus(message, isError) {
    var line = openDlg && openDlg.statusEl;
    if (!line) {
      return;
    }
    line.textContent = message || "";
    line.classList.toggle("is-error", !!isError);
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

  function renderLists() {
    renderOpenList();
    renderManageList();
  }

  // emptyNote is what a list says instead of rows: plainly that there are none
  // once that is known, and that it is still being fetched before then.
  function emptyNote(listEl, noteEl, none) {
    var empty = !profiles.length;
    listEl.hidden = empty;
    noteEl.hidden = !empty;
    if (empty) {
      noteEl.textContent = loadFailed
        ? "Saved connections could not be loaded."
        : loaded
          ? none
          : "Loading saved connections…";
    }
    return empty;
  }

  // One row, in either dialog: the name and what it dials as plain text, the
  // badges, then the buttons that act on it. The text is not a control of its
  // own — a second Tab stop that did what the button beside it does was noise —
  // but a click anywhere on the row still runs its main action, for the
  // pointer.
  function profileRow(p, actions, mainAction) {
    var row = document.createElement("li");
    row.className = "profile-row";

    var main = document.createElement("div");
    main.className = "profile-row-main";
    main.innerHTML =
      '<span class="profile-row-name"></span>' +
      '<span class="profile-row-target"></span>' +
      '<span class="profile-row-desc subtle"></span>';
    main.querySelector(".profile-row-name").textContent = p.name;
    main.querySelector(".profile-row-target").textContent = summarise(p);
    main.querySelector(".profile-row-desc").textContent = p.description || "";
    row.appendChild(main);

    var scope = document.createElement("span");
    scope.className = "profile-row-scope";
    scope.textContent = p.shared ? "Shared" : "Personal";
    row.appendChild(scope);

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

    actions.forEach(function (action) {
      var button = document.createElement("button");
      button.type = "button";
      button.className = "profile-row-action" + (action.className ? " " + action.className : "");
      button.textContent = action.text;
      button.setAttribute("aria-label", action.label);
      button.addEventListener("click", function (event) {
        event.stopPropagation();
        action.run();
      });
      row.appendChild(button);
    });

    row.classList.add("is-clickable");
    row.addEventListener("click", function (event) {
      if (!event.target.closest("button")) {
        mainAction();
      }
    });
    return row;
  }

  function renderOpenList() {
    if (!openDlg) {
      return;
    }
    var listEl = openDlg.listEl;
    listEl.textContent = "";
    if (!emptyNote(listEl, openDlg.emptyEl, "No saved connections yet.")) {
      profiles.forEach(function (p) {
        listEl.appendChild(
          profileRow(
            p,
            [
              {
                text: "Open",
                className: "profile-row-open",
                // The visible word is "Open", and the name begins with it, so
                // what a speech user says matches what a sighted user reads.
                label: "Open a session to " + p.name + " — " + summarise(p),
                run: function () {
                  choose(p);
                }
              }
            ],
            function () {
              choose(p);
            }
          )
        );
      });
    }
    renderSamples();
  }

  function renderManageList() {
    if (!manageDlg) {
      return;
    }
    var listEl = manageDlg.listEl;
    listEl.textContent = "";
    var none = sampleApps.length
      ? "No saved connections yet. Fill in the form below to add one, or pick a bundled sample app to fill in its host and port."
      : "No saved connections yet. Fill in the form below to add one.";
    if (emptyNote(listEl, manageDlg.emptyEl, none)) {
      return;
    }
    profiles.forEach(function (p) {
      listEl.appendChild(
        profileRow(
          p,
          [
            {
              text: "Edit",
              label: "Edit profile " + p.name,
              run: function () {
                fill(p);
              }
            },
            {
              text: "Delete",
              className: "danger",
              label: "Delete profile " + p.name,
              run: function () {
                remove(p);
              }
            }
          ],
          function () {
            fill(p);
          }
        )
      );
    });
  }

  // renderSamples offers the bundled sample apps as things to open, which is
  // all they are in this dialog. Opening one used to mean either having a
  // profile already or typing an address into a browser prompt, and the
  // address of a sample app — "sampleapp:<id>" on a port that is not 3270 — is
  // not something anybody is told. An instance with no mainframe to reach is
  // exactly the instance where the second session is hardest to open, which is
  // the wrong way round.
  function renderSamples() {
    if (!openDlg) {
      return;
    }
    var wrap = openDlg.samplesWrap;
    var list = openDlg.samplesEl;
    wrap.hidden = !sampleApps.length || !!document.body.dataset.enforcedTarget;
    list.textContent = "";
    if (wrap.hidden) {
      return;
    }
    sampleApps.forEach(function (app) {
      var row = document.createElement("li");
      row.className = "profile-row profile-sample-row is-clickable";
      row.innerHTML =
        '<div class="profile-row-main"><span class="profile-row-name"></span>' +
        '<span class="profile-row-target"></span></div>';
      row.querySelector(".profile-row-name").textContent = app.name;
      row.querySelector(".profile-row-target").textContent = "port " + app.port;

      var open = function () {
        chooseTarget({
          name: "",
          target: app.host + ":" + app.port,
          label: app.name
        });
      };
      var button = document.createElement("button");
      button.type = "button";
      button.className = "profile-row-action profile-row-open";
      button.textContent = "Open";
      button.setAttribute("aria-label", "Open a session to the " + app.name + " sample app");
      button.addEventListener("click", function (event) {
        event.stopPropagation();
        open();
      });
      row.appendChild(button);
      row.addEventListener("click", function (event) {
        if (!event.target.closest("button")) {
          open();
        }
      });
      list.appendChild(row);
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
     choosing one fills the connection in. That list lives in the profile
     editor and nowhere else: the Open dialog lists the same apps as things to
     start, and one dialog doing both is how it came to show them twice. */

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
        renderLists();
        clearForm();
        setStatus("Saved “" + payload.name + "”.");
        notify("Connection saved: " + payload.name, "success");
        profilesChanged();
      },
      function (err) {
        setStatus((err && err.message) || "Could not save the profile.", true);
      }
    );
  }

  // How many hosts an account has decides which chooser it is given for another
  // session — the selection screen once there is more than one to choose from,
  // this dialog otherwise — and the tab bar reads that once, when it loads.
  // Saving, deleting or importing a profile can move an account across that
  // line, so the tab bar is told to ask again; left alone, "+ New session"
  // would go on offering whichever chooser it had been given before.
  function profilesChanged() {
    var tabs = window.ThreeSeventyWeb && window.ThreeSeventyWeb.sessionTabs;
    if (tabs && typeof tabs.reload === "function") {
      tabs.reload().catch(function () {
        /* The tab bar is stale, not broken; its next load corrects it. */
      });
    }
  }

  function remove(p) {
    openDeleteConfirm(p);
  }

  function openDeleteConfirm(p) {
    pendingDelete = p;
    if (!deleteConfirmEl) {
      // build() always runs before a delete button exists to click, so this
      // is unreachable in practice — kept only so a missing dialog fails
      // open rather than leaving the button dead.
      performDelete(p);
      return;
    }
    if (deleteConfirmDesc) {
      deleteConfirmDesc.textContent = "Delete the connection profile “" + p.name + "”? This cannot be undone.";
    }
    deleteConfirmEl.hidden = false;
    // Opens on Cancel: a delete cannot be undone, so a stray Enter or Space as
    // the dialog appears has to be the harmless answer.
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      window.ThreeSeventyWeb.pushModal(deleteConfirmEl, closeDeleteConfirm, { initialFocus: deleteConfirmCancel });
    } else if (deleteConfirmCancel) {
      deleteConfirmCancel.focus();
    }
  }

  function closeDeleteConfirm() {
    pendingDelete = null;
    if (!deleteConfirmEl || deleteConfirmEl.hidden) {
      return;
    }
    deleteConfirmEl.hidden = true;
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
      window.ThreeSeventyWeb.popModal(deleteConfirmEl);
    }
  }

  function performDelete(p) {
    api("/api/profiles/delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: p.name })
    }).then(
      function (result) {
        profiles = result.profiles || [];
        renderLists();
        setStatus("Deleted “" + p.name + "”.");
        profilesChanged();
        // The button that was pressed went with its row, and focus with it.
        // The next row's Edit is the nearest thing left; with no rows, the
        // form is what the dialog has to offer.
        var next = manageDlg && manageDlg.listEl.querySelector("button");
        (next || (formEl && formEl.elements.name)).focus();
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
    closeOpenDialog();
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

  /* ----------------------------------------------------------- opening both */

  function pushDialog(root, close, initialFocus) {
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      // Escape, focus, the Tab trap, the background scroll lock and the focus
      // restore all belong to pushModal/popModal — see modal-utils.js.
      window.ThreeSeventyWeb.pushModal(root, close, { initialFocus: initialFocus });
    }
  }

  function popDialog(root) {
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
      window.ThreeSeventyWeb.popModal(root);
    }
  }

  function rememberOpener() {
    if (!openerEl) {
      openerEl = document.activeElement;
    }
  }

  function shown(dlg) {
    return !!dlg && !dlg.root.hidden;
  }

  // The menu item's own click is the opener, not whatever happened to have
  // focus: a menu opened with the pointer leaves focus where it was. A click
  // synthesised by Enter or Space reports a detail of 0, which is how the menu
  // bar tells the two apart as well.
  function startedFromMenu(event) {
    if (!openerEl) {
      openerEl = event.currentTarget;
      openerKeyboard = event.detail === 0;
    }
  }

  // A dialog opened from a menu item has nowhere to hand the keyboard back to:
  // the menu has closed by the time it does, and the item went with it. Left
  // alone, focus falls to the page body and the next key does nothing visible.
  //
  // Where it goes instead follows how the item was chosen. Somebody who got
  // there from the keyboard goes back to the menu's own button, the place they
  // started from. Somebody who clicked is working with the pointer and the
  // terminal, and the menu bar already hands them back to the terminal after
  // every item that does not open a dialog; a menu button holding focus would
  // turn their next Enter into "open the Session menu" rather than a keystroke
  // for the host. A dialog started with Alt+N or the palette, where nothing
  // visible had focus either, also ends on the terminal.
  function settleFocus() {
    var opener = openerEl;
    var viaKeyboard = openerKeyboard;
    openerEl = null;
    openerKeyboard = false;
    var active = document.activeElement;
    var lost =
      !active ||
      active === document.body ||
      active === document.documentElement ||
      (openDlg && openDlg.root.contains(active)) ||
      (manageDlg && manageDlg.root.contains(active));
    if (!lost) {
      return;
    }
    if (opener && document.contains(opener) && typeof opener.closest === "function") {
      var menu = opener.closest("[data-app-menu]");
      var target = menu ? (viaKeyboard ? menu.querySelector("[data-app-menu-trigger]") : null) : opener;
      if (target && target.getClientRects().length) {
        target.focus();
        if (document.activeElement === target) {
          return;
        }
      }
    }
    var terminal = window.ThreeSeventyWeb && window.ThreeSeventyWeb.terminal;
    if (terminal && typeof terminal.focusTerminal === "function") {
      terminal.focusTerminal();
    }
  }

  function hideDialog(dlg) {
    if (!shown(dlg)) {
      return false;
    }
    dlg.root.hidden = true;
    popDialog(dlg.root);
    return true;
  }

  /* ------------------------------------------------------ Open a connection */

  // firstUseful is where the keyboard starts in the Open dialog: the first
  // thing it can open. The list arrives after the dialog does, so this runs
  // again once it has — and only if nobody has moved on in the meantime.
  function focusFirstUseful() {
    if (!shown(openDlg)) {
      return;
    }
    var active = document.activeElement;
    if (active && active !== openDlg.content && openDlg.root.contains(active)) {
      return;
    }
    var target =
      openDlg.listEl.querySelector("button") ||
      openDlg.samplesEl.querySelector("button") ||
      (!openDlg.direct.hidden && openDlg.direct.elements.target) ||
      openDlg.root.querySelector("[data-profiles-manage-link]");
    if (target) {
      target.focus();
    }
  }

  function openConnection(picker) {
    if (!openDlg) {
      return;
    }
    onPick = picker || null;
    rememberOpener();
    openDlg.root.hidden = false;
    openDlg.direct.hidden = !!document.body.dataset.enforcedTarget;
    openDlg.direct.reset();
    setOpenStatus("");
    renderOpenList();
    // The dialog itself takes focus first, so the title is what is announced,
    // and the first control takes it over once the list has been drawn.
    pushDialog(openDlg.root, closeOpenDialog, openDlg.content);
    load().then(focusFirstUseful, function (err) {
      setOpenStatus((err && err.message) || "Could not load saved connections.", true);
      focusFirstUseful();
    });
    // When the list is already known there is nothing to wait for. Before it
    // is, the only control there is to land on is the host box, and putting
    // the keyboard there only to move it a moment later is worse than leaving
    // it on the dialog.
    if (loaded) {
      focusFirstUseful();
    }
  }

  function closeOpenDialog() {
    if (hideDialog(openDlg)) {
      settleFocus();
    }
  }

  // The Open dialog's way to the other one. The first closes before the second
  // opens, so there is never a dialog stacked on a dialog, and the keyboard
  // ends up back where it started when the second is closed.
  function handOffToManage() {
    hideDialog(openDlg);
    manageConnections(null);
  }

  /* --------------------------------------------------- Manage connections */

  function manageConnections(prefill) {
    if (!manageDlg) {
      return;
    }
    rememberOpener();
    manageDlg.root.hidden = false;
    showBrowserImport();
    renderManageList();
    clearForm();
    // A phone's software keyboard would cover the list the moment a text field
    // took focus, so there the dialog takes it and the person picks.
    var coarse = window.matchMedia && window.matchMedia("(pointer: coarse)").matches;
    pushDialog(manageDlg.root, closeManageDialog, coarse && !prefill ? manageDlg.content : formEl.elements.name);
    if (prefill) {
      fill(prefill);
    }
    load().catch(function (err) {
      setStatus((err && err.message) || "Could not load profiles.", true);
    });
  }

  function closeManageDialog() {
    closeDeleteConfirm();
    if (hideDialog(manageDlg)) {
      settleFocus();
    }
  }

  // Both dialogs share one close, so a caller that does not care which is open
  // does not have to know.
  function close() {
    closeOpenDialog();
    closeManageDialog();
  }

  function isOpen() {
    return shown(openDlg) || shown(manageDlg);
  }

  function parseTarget(raw) {
    var value = String(raw || "").trim();
    var tls = false, skipVerify = false, luName = "";
    if (value.startsWith("L:")) { tls = true; value = value.slice(2); }
    if (value.startsWith("Y:")) { skipVerify = true; value = value.slice(2); }
    if (value.includes("@")) { var parts = value.split("@"); luName = parts.shift(); value = parts.join("@"); }
    var host = value, port = 3270;
    var ipv6 = value.match(/^\[([^\]]+)\](?::(\d+))?$/);
    var regular = value.match(/^([^:]+):(\d+)$/);
    var sample = value.match(/^(sampleapp:[^:]+):(\d+)$/);
    var match = ipv6 || sample || regular;
    if (match) { host = match[1]; port = Number(match[2] || 3270); }
    return { host: host, port: port, tls: tls, skipVerify: skipVerify, luName: luName };
  }

  function showBrowserImport() {
    var box = manageDlg && manageDlg.root.querySelector("[data-legacy-import]");
    if (!box) return;
    try {
      var items = JSON.parse(localStorage.getItem("3270Web.savedHosts.v1") || "[]");
      var done = localStorage.getItem("3270Web.hostsImported." + (document.body.dataset.account || "local"));
      box.hidden = !Array.isArray(items) || !items.length || done === "true";
    } catch (_) { box.hidden = true; }
  }

  async function importConnections(items) {
    if (!Array.isArray(items) || items.length > 100) throw new Error("Choose a connection file containing up to 100 entries.");
    var imported = 0;
    for (var entry of items) {
      var raw = typeof entry === "string" ? entry : entry.hostname;
      var payload = raw ? Object.assign(parseTarget(raw), { name: typeof entry === "string" ? entry : (entry.name || raw) }) : Object.assign({}, entry);
      // Never promote an import into shared hosts or carry another user's audience.
      payload.publish = false; payload.shared = false; payload.users = []; payload.groups = []; payload.roles = []; payload.notOffered = false;
      var base = String(payload.name || payload.host || "Imported connection").slice(0, 55);
      payload.name = base;
      var suffix = 2;
      while (profiles.some(function (p) { return p.name.toLowerCase() === payload.name.toLowerCase(); })) payload.name = base + " (" + suffix++ + ")";
      if (!payload.host) throw new Error("An entry has no host. " + imported + " connection(s) were imported; originals were kept.");
      var result = await api("/api/profiles", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload) });
      profiles = result.profiles || []; imported++;
    }
    renderLists(); setStatus("Imported " + imported + " personal connection(s). Originals were kept.");
    profilesChanged();
    return imported;
  }

  function saveCurrent(connectForm) {
    var target = parseTarget(connectForm.elements.hostname.value);
    manageConnections(Object.assign(target, {
      name: "", tls: connectForm.elements.tls.checked || target.tls,
      luName: connectForm.elements.luName.value || target.luName,
      model: connectForm.elements.model.value,
      codePage: connectForm.elements.codePage.value
    }));
    setStatus("Name this connection and save it to your personal list.");
    formEl.elements.name.focus();
  }

  /* ----------------------------------------------------------------- markup */

  function newRoot(kind) {
    var root = document.createElement("div");
    root.className = "profiles-modal";
    root.hidden = true;
    root.setAttribute("data-profiles-modal", "");
    root.setAttribute("data-profiles-dialog", kind);
    return root;
  }

  function wireClosers(root, close) {
    var closers = root.querySelectorAll("[data-profiles-close]");
    for (var i = 0; i < closers.length; i++) {
      closers[i].addEventListener("click", close);
    }
  }

  function buildOpenDialog() {
    var root = newRoot("open");
    root.innerHTML = [
      '<div class="profiles-modal-backdrop" data-profiles-close></div>',
      '<div class="profiles-modal-content" role="dialog" aria-modal="true" aria-labelledby="profiles-open-title" tabindex="-1">',
      '  <div class="profiles-modal-header">',
      '    <h3 id="profiles-open-title">Open a connection</h3>',
      '    <button type="button" data-profiles-close>Close</button>',
      "  </div>",
      '  <div class="profiles-modal-body">',
      '    <section class="profiles-section" aria-labelledby="profiles-open-saved-title">',
      '      <h4 id="profiles-open-saved-title">Saved connections</h4>',
      '      <p class="subtle profiles-empty" data-profiles-empty hidden></p>',
      '      <ul class="profiles-list" role="list" data-profiles-list></ul>',
      '      <p class="profiles-status" data-profiles-open-status role="status" aria-live="polite"></p>',
      "    </section>",
      // Hidden on an instance that names its one host for everybody, and on
      // one with no samples to offer.
      '    <section class="profiles-section profiles-samples" data-profiles-samples aria-labelledby="profiles-open-samples-title" hidden>',
      '      <div class="profiles-section-head">',
      '        <h4 id="profiles-open-samples-title">Sample apps</h4>',
      '        <p class="subtle">Servers 3270Web starts itself, so there is no mainframe to reach.</p>',
      "      </div>",
      '      <ul class="profiles-sample-list" role="list" data-profiles-sample-list></ul>',
      "    </section>",
      // The typed-address path. It lives in the dialog rather than in a
      // window.prompt because a prompt cannot be styled, cannot say what a
      // valid address looks like, and does not open at all inside a sandboxed
      // frame — which is where an embedded terminal runs.
      '    <form class="profiles-direct" data-profiles-direct hidden>',
      '      <div class="profiles-direct-row">',
      '        <label>Or connect to a host<input name="target" type="text" placeholder="hostname:port" autocomplete="off" autocapitalize="none" spellcheck="false"></label>',
      '        <button type="submit">Connect</button>',
      "      </div>",
      "    </form>",
      "  </div>",
      '  <div class="profiles-modal-footer">',
      '    <button type="button" class="quiet-action" data-profiles-manage-link>Manage connections…</button>',
      "  </div>",
      "</div>"
    ].join("");
    document.body.appendChild(root);

    var dlg = {
      root: root,
      content: root.querySelector(".profiles-modal-content"),
      listEl: root.querySelector("[data-profiles-list]"),
      emptyEl: root.querySelector("[data-profiles-empty]"),
      statusEl: root.querySelector("[data-profiles-open-status]"),
      samplesWrap: root.querySelector("[data-profiles-samples]"),
      samplesEl: root.querySelector("[data-profiles-sample-list]"),
      direct: root.querySelector("[data-profiles-direct]")
    };
    wireClosers(root, closeOpenDialog);
    root.querySelector("[data-profiles-manage-link]").addEventListener("click", handOffToManage);
    dlg.direct.addEventListener("submit", function (event) {
      event.preventDefault();
      var typed = dlg.direct.elements.target.value.trim();
      if (!typed) {
        dlg.direct.elements.target.focus();
        return;
      }
      chooseTarget({ name: "", target: typed, label: typed });
    });
    return dlg;
  }

  function buildManageDialog() {
    var root = newRoot("manage");
    root.innerHTML = [
      '<div class="profiles-modal-backdrop" data-profiles-close></div>',
      '<div class="profiles-modal-content is-wide" role="dialog" aria-modal="true" aria-labelledby="profiles-manage-title" tabindex="-1">',
      '  <div class="profiles-modal-header">',
      '    <h3 id="profiles-manage-title">Manage connections</h3>',
      '    <button type="button" data-profiles-close>Close</button>',
      "  </div>",
      '  <div class="profiles-modal-body">',
      '    <section class="profiles-section" aria-labelledby="profiles-manage-saved-title">',
      '      <div class="profiles-section-head">',
      '        <h4 id="profiles-manage-saved-title">Saved connections</h4>',
      '        <div class="profiles-io">',
      '          <button type="button" data-import-connections>Import connections</button>',
      '          <button type="button" data-export-connections>Export connections</button>',
      '          <input type="file" data-import-connections-file accept="application/json,.json" hidden>',
      "        </div>",
      "      </div>",
      '      <section class="legacy-import" data-legacy-import hidden><p>Connections saved in this browser can be imported into your personal saved connections. The original browser list will be kept.</p><button type="button" data-import-browser>Import browser connections</button></section>',
      '      <p class="subtle profiles-empty" data-profiles-empty hidden></p>',
      '      <ul class="profiles-list" role="list" data-profiles-list></ul>',
      "    </section>",
      '    <form class="profiles-form" data-profiles-form aria-labelledby="profiles-manage-form-title">',
      '      <h4 id="profiles-manage-form-title">Add or edit a connection</h4>',
      // Hidden on an instance with no bundled samples, which is why it is a
      // wrapper rather than the label itself: .profiles-form label is
      // display:flex, and [hidden] loses to it.
      '      <div class="profiles-sample" data-profiles-sample-field hidden>',
      // No "optional" tag: the default option says so in words, and the tag
      // would wrap onto its own line above the select.
      '        <label>Bundled sample app',
      '          <select name="sampleApp" aria-describedby="profiles-sample-hint">',
      '            <option value="">A mainframe on the network</option>',
      "          </select>",
      "        </label>",
      '        <p class="subtle profiles-sample-hint" id="profiles-sample-hint">Choosing one fills in the host and port below. The sample apps are TN3270 servers 3270Web starts itself, so a profile naming one connects with no mainframe to reach.</p>',
      "      </div>",
      '      <div class="profiles-form-grid">',
      '        <label>Name<input name="name" type="text" required maxlength="64" placeholder="CICS Production"></label>',
      '        <label>Host<input name="host" type="text" required placeholder="mainframe.example.com"></label>',
      '        <label>Port<input name="port" type="number" min="1" max="65535" value="3270"></label>',
      "      </div>",
      '      <label class="profiles-check"><input name="tls" type="checkbox"> Use TLS</label>',
      '      <details class="profiles-advanced"><summary>Terminal options</summary><div class="profiles-form-grid">',
      '        <label>LU name <span class="subtle">optional</span><input name="luName" type="text" placeholder="TCP00042"></label>',
      '        <label>Model <span class="subtle">optional</span><input name="model" type="text" list="profile-models" placeholder="3279-4-E"></label>',
      '        <label>Code page <span class="subtle">optional</span><input name="codePage" type="text" list="profile-codepages" placeholder="cp037"></label>',
      "      </div>",
      '      <label class="profiles-check"><input name="skipVerify" type="checkbox" disabled> Skip certificate verification <span class="subtle">weaker — only for self-signed test hosts</span></label>',
      "      </details>",
      '      <label>Description <span class="subtle">optional</span><input name="description" type="text" maxlength="200"></label>',
      // Publishing and its audience are one block, shown only to an
      // administrator: on an instance with one operator there is nobody to
      // publish to, and for anybody else a save is their own copy.
      '      <fieldset class="profiles-share" data-profiles-share hidden>',
      "        <legend>Who this host is for</legend>",
      '        <label class="profiles-check"><input name="publish" type="checkbox"> Share with everyone <span class="subtle">otherwise this is your own copy</span></label>',
      '        <div class="profiles-form-grid" data-profiles-audience>',
      '          <label>Groups <span class="subtle">comma separated</span><input name="audienceGroups" type="text" list="profile-known-groups" placeholder="payments, ops"></label>',
      '          <label>Users <span class="subtle">comma separated</span><input name="audienceUsers" type="text" placeholder="alice, bob"></label>',
      '          <label>Roles<select name="audienceRoles"><option value="">Any role</option><option value="user">Users only</option><option value="admin">Administrators only</option></select></label>',
      "        </div>",
      '        <datalist id="profile-known-groups" data-profiles-known-groups></datalist>',
      '        <p class="subtle profiles-audience-hint">Leave all three empty and everyone sees this host. Naming any of them narrows it to whoever matches at least one.</p>',
      "      </fieldset>",
      // Sticks to the bottom of the dialog's scrolling area, so Save and the
      // line that says whether it worked stay in view however far down the
      // form somebody has scrolled.
      '      <div class="profiles-form-actions">',
      '        <span class="profiles-status" data-profiles-status aria-live="polite"></span>',
      '        <button type="button" data-profiles-clear>Clear</button>',
      '        <button type="submit">Save profile</button>',
      "      </div>",
      '      <datalist id="profile-models"><option value="3279-2-E"><option value="3279-3-E"><option value="3279-4-E"><option value="3279-5-E"></datalist>',
      '      <datalist id="profile-codepages"><option value="cp037"><option value="cp1140"><option value="cp285"></datalist>',
      "    </form>",
      "  </div>",
      // Reuses the .settings-confirm-* classes every other in-panel confirm
      // in the app (saved hosts, restart, chaos runs) is built from, so this
      // one more delete looks like the ones beside it instead of like a
      // browser dialog dropped into a branded page.
      '  <div class="settings-confirm-modal" data-profiles-delete-confirm hidden>',
      '    <div class="settings-confirm-backdrop" data-profiles-delete-cancel></div>',
      '    <div class="settings-confirm-content" role="dialog" aria-modal="true" aria-labelledby="profiles-delete-title" aria-describedby="profiles-delete-desc">',
      '      <h4 id="profiles-delete-title">Delete connection profile?</h4>',
      '      <p id="profiles-delete-desc" data-profiles-delete-desc></p>',
      '      <div class="settings-confirm-actions">',
      '        <button type="button" data-profiles-delete-cancel data-profiles-delete-cancel-button>Cancel</button>',
      '        <button type="button" class="danger" data-profiles-delete-accept>Delete profile</button>',
      "      </div>",
      "    </div>",
      "  </div>",
      "</div>"
    ].join("");
    document.body.appendChild(root);

    var dlg = {
      root: root,
      content: root.querySelector(".profiles-modal-content"),
      listEl: root.querySelector("[data-profiles-list]"),
      emptyEl: root.querySelector("[data-profiles-empty]")
    };
    formEl = root.querySelector("[data-profiles-form]");
    statusEl = root.querySelector("[data-profiles-status]");
    deleteConfirmEl = root.querySelector("[data-profiles-delete-confirm]");
    deleteConfirmDesc = root.querySelector("[data-profiles-delete-desc]");
    deleteConfirmCancel = root.querySelector("[data-profiles-delete-cancel-button]");
    deleteConfirmAccept = root.querySelector("[data-profiles-delete-accept]");

    root.querySelector("[data-import-browser]").addEventListener("click", async function (event) {
      var button = event.currentTarget; button.disabled = true;
      try {
        await importConnections(JSON.parse(localStorage.getItem("3270Web.savedHosts.v1") || "[]"));
        try { localStorage.setItem("3270Web.hostsImported." + (document.body.dataset.account || "local"), "true"); } catch (_) {}
        showBrowserImport();
      } catch (err) { setStatus(err.message || "Import failed. Originals were kept.", true); }
      finally { button.disabled = false; }
    });
    var importFile = root.querySelector("[data-import-connections-file]");
    root.querySelector("[data-import-connections]").addEventListener("click", function () { importFile.click(); });
    importFile.addEventListener("change", async function () {
      var file = importFile.files[0]; if (!file) return;
      try {
        if (file.size > 1024 * 1024) throw new Error("Choose a file under 1 MB.");
        var payload = JSON.parse(await file.text());
        await importConnections(Array.isArray(payload) ? payload : (payload.profiles || payload.hosts));
      } catch (err) { setStatus(err.message || "Could not import that file.", true); }
      finally { importFile.value = ""; }
    });
    root.querySelector("[data-export-connections]").addEventListener("click", function () {
      var url = URL.createObjectURL(new Blob([JSON.stringify({ profiles: profiles }, null, 2)], { type: "application/json" }));
      var link = document.createElement("a"); link.href = url; link.download = "3270web-connections.json"; link.click();
      window.setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
      setStatus("Connections exported.");
    });

    var deleteCancelers = deleteConfirmEl.querySelectorAll("[data-profiles-delete-cancel]");
    for (var d = 0; d < deleteCancelers.length; d++) {
      deleteCancelers[d].addEventListener("click", closeDeleteConfirm);
    }
    deleteConfirmAccept.addEventListener("click", function () {
      var target = pendingDelete;
      closeDeleteConfirm();
      if (target) {
        performDelete(target);
      }
    });

    formEl.addEventListener("submit", save);
    formEl.elements.tls.addEventListener("change", syncSkipVerify);
    formEl.elements.sampleApp.addEventListener("change", applySampleApp);
    formEl.elements.host.addEventListener("input", syncSampleSelect);
    root.querySelector("[data-profiles-clear]").addEventListener("click", clearForm);
    wireClosers(root, closeManageDialog);
    return dlg;
  }

  document.addEventListener("DOMContentLoaded", function () {
    openDlg = buildOpenDialog();
    manageDlg = buildManageDialog();
    syncShareVisibility();
    fillSampleApps();

    // The Session menu's "Open a connection…". It starts a session the way
    // "+ New session" does, because that is what it is: the tab bar owns what
    // a choice turns into — including where an account that picks its host
    // from the selection screen is sent instead — so the menu asks it rather
    // than keeping a second copy of the rule here.
    var openers = document.querySelectorAll("[data-profiles-open]");
    for (var i = 0; i < openers.length; i++) {
      openers[i].addEventListener("click", function (event) {
        var tabs = window.ThreeSeventyWeb && window.ThreeSeventyWeb.sessionTabs;
        if (!tabs || typeof tabs.open !== "function") {
          notify("Opening another session is not available on this page.", "warning");
          return;
        }
        // The tab bar's own button is disabled at the session limit, and says
        // why in its title; the menu item has to honour the same limit.
        var newButton = document.querySelector("[data-session-new]");
        if (newButton && newButton.disabled) {
          notify(newButton.getAttribute("title") || "No more sessions can be opened.", "warning");
          return;
        }
        startedFromMenu(event);
        tabs.open();
        if (!isOpen()) {
          openerEl = null;
          openerKeyboard = false;
        }
      });
    }

    var managers = document.querySelectorAll("[data-profiles-manage]");
    for (var m = 0; m < managers.length; m++) {
      managers[m].addEventListener("click", function (event) {
        startedFromMenu(event);
        manageConnections(null);
      });
    }

    // On the connect page, choosing a profile submits the connect form with
    // the profile name so the server applies its TLS/LU/model settings.
    var connectForm = document.getElementById("connect-form");
    var pickTrigger = document.querySelector("[data-profiles-connect]");
    if (pickTrigger && connectForm) {
      pickTrigger.addEventListener("click", function () {
        openConnection(function (choice) {
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
          if (connectForm.elements.advanced) connectForm.elements.advanced.checked = false;
          connectForm.requestSubmit();
        });
      });
    }
  });

  window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
  window.ThreeSeventyWeb.connectionProfiles = {
    saveCurrent: saveCurrent,
    // open("pick", callback) is the Open dialog and anything else is Manage;
    // kept so a caller written for the one dialog still reaches the right one.
    open: function (mode, picker) {
      if (mode === "pick") {
        openConnection(picker);
      } else {
        manageConnections(null);
      }
    },
    manage: function () {
      manageConnections(null);
    },
    close: close,
    isOpen: isOpen,
    load: load,
    list: function () {
      return profiles.slice();
    },
    // Used by the session tab bar's "+" so opening another session offers
    // the same profiles rather than a bare hostname prompt.
    pick: function (callback) {
      openConnection(callback);
    }
  };
})();
