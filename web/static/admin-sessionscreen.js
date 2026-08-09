/* Session-screen management.
   -------------------------------------------------------------------------
   Two things make up the selection screen an operator meets: the preset
   host list (and who each entry is offered to), and the branding at the
   top. This page manages both, against /api/admin/profiles and
   /api/admin/menu-branding.

   The audience model is the server's: empty means everyone, and naming
   users, groups or roles narrows the preset to whoever matches at least
   one. This page only edits those lists; internal/users decides matching. */
(function () {
  'use strict';

  var rows = document.querySelector('[data-preset-rows]');
  if (!rows) return;

  var statusEl = document.querySelector('[data-ss-status]');
  var authEnabled = document.body.getAttribute('data-auth-enabled') === 'true';
  var dialogs = {};
  var lastFocus = null;
  var knownGroups = [];
  var knownUsers = [];
  // The bundled sample apps a preset may point at instead of a mainframe.
  var sampleApps = [];
  // The published set as last loaded, so the sample-app adder can tell which
  // of them are already on the list.
  var published = [];
  var editingName = null;

  document.querySelectorAll('[data-dialog]').forEach(function (el) {
    dialogs[el.getAttribute('data-dialog')] = el;
  });

  /* ---------------------------------------------------------------- utils */

  function setStatus(message, state) {
    if (!statusEl) return;
    statusEl.textContent = message || '';
    if (state) {
      statusEl.setAttribute('data-state', state);
    } else {
      statusEl.removeAttribute('data-state');
    }
    if (message) {
      try {
        statusEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      } catch (_) {
        statusEl.scrollIntoView();
      }
    }
  }

  function api(method, url, body) {
    var options = {
      method: method,
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
      credentials: 'same-origin'
    };
    if (body !== undefined) {
      options.headers['Content-Type'] = 'application/json';
      options.body = JSON.stringify(body);
    }
    return fetch(url, options).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (data) {
        if (!response.ok) {
          throw new Error(data.error || ('Request failed (' + response.status + ')'));
        }
        return data;
      });
    });
  }

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function openDialog(name) {
    var dialog = dialogs[name];
    if (!dialog) return;
    lastFocus = document.activeElement;
    dialog.hidden = false;
    var focusable = dialog.querySelector('input, select') ||
      dialog.querySelector('button:not(.admin-dialog-close)');
    if (focusable) focusable.focus();
    dialog.scrollTop = 0;
  }

  function closeDialogs() {
    Object.keys(dialogs).forEach(function (key) {
      dialogs[key].hidden = true;
    });
    if (lastFocus && lastFocus.focus) lastFocus.focus();
    lastFocus = null;
  }

  document.addEventListener('keydown', function (event) {
    if (event.key === 'Escape') closeDialogs();
  });
  document.querySelectorAll('[data-dialog-cancel]').forEach(function (button) {
    button.addEventListener('click', closeDialogs);
  });
  Object.keys(dialogs).forEach(function (key) {
    dialogs[key].addEventListener('mousedown', function (event) {
      if (event.target === dialogs[key]) closeDialogs();
    });
  });

  function confirmThen(title, description, action) {
    var dialog = dialogs.confirm;
    if (!dialog) { action(); return; }
    dialog.querySelector('[data-confirm-title]').textContent = title;
    dialog.querySelector('[data-confirm-desc]').textContent = description;
    var accept = dialog.querySelector('[data-confirm-accept]');
    var fresh = accept.cloneNode(true);
    accept.parentNode.replaceChild(fresh, accept);
    fresh.addEventListener('click', function () {
      closeDialogs();
      action();
    });
    openDialog('confirm');
  }

  /* -------------------------------------------------------------- presets */

  function splitList(value) {
    return (value || '').split(',').map(function (s) { return s.trim(); })
      .filter(function (s) { return s !== ''; });
  }

  function audienceCell(p) {
    var td = el('td');
    var users = p.users || [];
    var groups = p.groups || [];
    var roles = p.roles || [];
    if (!users.length && !groups.length && !roles.length) {
      td.appendChild(el('span', 'admin-groups-none', 'Everyone'));
      return td;
    }
    var wrap = el('div', 'admin-groups');
    groups.forEach(function (name) {
      wrap.appendChild(el('span', 'admin-group-tag', name));
    });
    users.forEach(function (name) {
      wrap.appendChild(el('span', 'admin-group-tag admin-tag--user', name));
    });
    roles.forEach(function (name) {
      wrap.appendChild(el('span', 'admin-group-tag admin-tag--role',
        name === 'admin' ? 'administrators' : 'users'));
    });
    td.appendChild(wrap);
    return td;
  }

  function connectionFacts(p) {
    var facts = [];
    if (p.tls) facts.push(p.skipVerify ? 'TLS (unverified)' : 'TLS');
    if (p.luName) facts.push('LU ' + p.luName);
    if (p.model) facts.push(p.model);
    if (p.codePage) facts.push(p.codePage);
    return facts.join(' · ') || '—';
  }

  function renderRow(p) {
    var tr = el('tr');

    var nameCell = el('td');
    nameCell.appendChild(el('div', 'admin-username', p.name));
    if (p.description) nameCell.appendChild(el('div', 'admin-session-id', p.description));
    tr.appendChild(nameCell);

    // A bundled sample app is shown by the name it was chosen by; the
    // "sampleapp:app1:3271" it is addressed as is an internal detail.
    var sample = sampleAppFor(p.host);
    tr.appendChild(el('td', 'sessions-host',
      sample ? sample.name : p.host + ':' + (p.port || 3270)));
    tr.appendChild(el('td', 'presets-conn', connectionFacts(p)));
    tr.appendChild(audienceCell(p));

    var actionsCell = el('td', 'admin-actions-cell');
    var actions = el('div', 'admin-actions');
    var edit = el('button', '', 'Edit');
    edit.type = 'button';
    edit.addEventListener('click', function () { openPreset(p); });
    actions.appendChild(edit);
    var remove = el('button', 'danger', 'Remove');
    remove.type = 'button';
    remove.addEventListener('click', function () {
      confirmThen('Remove ' + p.name + '?',
        'The preset disappears from the selection screen and the connect page for everybody it was offered to. Nobody’s open session is touched.',
        function () {
          api('POST', '/api/admin/profiles/delete', { name: p.name })
            .then(function () {
              setStatus(p.name + ' removed.', 'ok');
              load();
            })
            .catch(function (error) { setStatus(error.message, 'error'); });
        });
    });
    actions.appendChild(remove);
    actionsCell.appendChild(actions);
    tr.appendChild(actionsCell);
    return tr;
  }

  function render(profiles) {
    rows.textContent = '';
    if (!profiles.length) {
      var tr = el('tr');
      var td = el('td', 'admin-empty');
      td.colSpan = 5;
      td.appendChild(el('strong', '', 'No presets yet'));
      td.appendChild(document.createTextNode(sampleApps.length
        ? 'Add one to put a mainframe on the selection screen, or add a bundled sample app to have something to connect to without one.'
        : 'Add one to put a mainframe on the selection screen.'));
      tr.appendChild(td);
      rows.appendChild(tr);
      return;
    }
    profiles.forEach(function (p) { rows.appendChild(renderRow(p)); });
  }

  /* ---------------------------------------------------------- sample apps */

  // The bundled sample apps, offered as a list so a preset can name one
  // without anybody having to know the "sampleapp:<id>" form. Choosing one
  // fills the host and port in; typing over them afterwards is allowed, and
  // puts the select back to "a mainframe on the network".

  function fillSampleApps() {
    var field = document.querySelector('[data-preset-sample-field]');
    var select = document.querySelector('[data-preset-sample]');
    if (!field || !select) return;
    field.hidden = !sampleApps.length;

    while (select.options.length > 1) select.remove(1);
    sampleApps.forEach(function (app) {
      var option = document.createElement('option');
      option.value = app.host;
      option.textContent = app.name;
      option.setAttribute('data-port', app.port);
      select.appendChild(option);
    });
  }

  function sampleAppFor(hostValue) {
    var host = String(hostValue || '').trim().toLowerCase();
    for (var i = 0; i < sampleApps.length; i++) {
      if (String(sampleApps[i].host).toLowerCase() === host) return sampleApps[i];
    }
    return null;
  }

  // The same apps again, this time above the list rather than inside the
  // dialog: choosing one publishes it there and then.
  //
  // Nothing about a sample app preset is a decision — the host, the port and
  // the name are fixed, and TLS against a plaintext listener on loopback
  // cannot work — so putting it behind a form somebody has to fill in and
  // save is asking for agreement to values they were never offered a choice
  // about. An instance being evaluated or taught on has these and no
  // mainframe, and this is how its host list gets populated.

  function publishedSampleHost(hostValue) {
    var host = String(hostValue || '').trim().toLowerCase();
    for (var i = 0; i < published.length; i++) {
      if (String(published[i].host || '').trim().toLowerCase() === host) return published[i];
    }
    return null;
  }

  // A name nobody else is using. The store upserts by name, so a sample app
  // whose name collides with a mainframe preset would replace it silently.
  function freePresetName(preferred) {
    // No prototype, so a preset named "constructor" is a name and not a hit.
    var taken = Object.create(null);
    published.forEach(function (p) { taken[String(p.name || '').toLowerCase()] = true; });
    if (!taken[preferred.toLowerCase()]) return preferred;
    for (var n = 2; n < 100; n++) {
      var candidate = preferred + ' ' + n;
      // The server refuses a name past 64 characters, so the suffix has to
      // come out of the name rather than be added to it.
      if (candidate.length > 64) {
        candidate = preferred.slice(0, 64 - (' ' + n).length) + ' ' + n;
      }
      if (!taken[candidate.toLowerCase()]) return candidate;
    }
    return preferred;
  }

  var sampleAdd = document.querySelector('[data-preset-sample-add]');

  function fillSampleAppAdder() {
    if (!sampleAdd) return;
    sampleAdd.hidden = !sampleApps.length;

    while (sampleAdd.options.length > 1) sampleAdd.remove(1);
    sampleApps.forEach(function (app) {
      var option = document.createElement('option');
      option.value = app.host;
      var already = !!publishedSampleHost(app.host);
      // Said on the option rather than by hiding it: an app that is missing
      // from the list reads as one this instance does not have.
      option.textContent = already ? app.name + ' (already added)' : app.name;
      option.disabled = already;
      sampleAdd.appendChild(option);
    });
    sampleAdd.value = '';
  }

  if (sampleAdd) {
    sampleAdd.addEventListener('change', function () {
      var chosen = sampleAppFor(sampleAdd.value);
      sampleAdd.value = '';
      if (!chosen) return;
      var name = freePresetName(chosen.name);
      api('POST', '/api/admin/profiles', {
        name: name,
        host: chosen.host,
        port: chosen.port,
        // Offered to everybody, which is what the audience fields mean when
        // they are empty. Narrowing it is an edit away, and guessing an
        // audience nobody asked for would hide the preset from the person
        // who just added it.
        groups: [],
        users: [],
        roles: []
      })
        .then(function () {
          setStatus(name + ' added to the selection screen.', 'ok');
          return load();
        })
        .catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  var sampleSelect = document.querySelector('[data-preset-sample]');
  if (sampleSelect) {
    sampleSelect.addEventListener('change', function () {
      if (!presetForm) return;
      var chosen = sampleAppFor(sampleSelect.value);
      if (!chosen) return;
      presetForm.querySelector('#preset-host').value = chosen.host;
      presetForm.querySelector('#preset-port').value = chosen.port;
      // A sample app is a plaintext listener on loopback that this process
      // started. TLS against it cannot succeed, so leaving a tick behind from
      // a previous edit would only produce a preset that fails to connect.
      presetForm.querySelector('[data-preset-tls]').checked = false;
      presetForm.querySelector('[data-preset-skipverify]').checked = false;
      if (!presetForm.querySelector('#preset-name').value.trim()) {
        presetForm.querySelector('#preset-name').value = chosen.name;
      }
    });
  }

  var hostInput = document.querySelector('#preset-host');
  if (hostInput && sampleSelect) {
    hostInput.addEventListener('input', function () {
      var chosen = sampleAppFor(hostInput.value);
      sampleSelect.value = chosen ? chosen.host : '';
    });
  }

  function fillDatalist(selector, names) {
    var list = document.querySelector(selector);
    if (!list) return;
    list.textContent = '';
    (names || []).forEach(function (name) {
      var option = document.createElement('option');
      option.value = name;
      list.appendChild(option);
    });
  }

  function load() {
    return api('GET', '/api/admin/profiles')
      .then(function (data) {
        knownGroups = data.groups || [];
        knownUsers = data.usernames || [];
        sampleApps = data.sampleApps || [];
        published = data.profiles || [];
        fillDatalist('[data-preset-known-groups]', knownGroups);
        fillDatalist('[data-preset-known-users]', knownUsers);
        fillSampleApps();
        fillSampleAppAdder();
        // With nobody to narrow an audience to, the fieldset would only
        // mislead: everything published is for the one operator.
        var audience = document.querySelector('[data-preset-audience]');
        if (audience) audience.hidden = !authEnabled;
        render(data.profiles || []);
      })
      .catch(function (error) {
        rows.textContent = '';
        var tr = el('tr');
        var td = el('td', 'admin-empty');
        td.colSpan = 5;
        td.appendChild(el('strong', '', 'Could not load presets'));
        td.appendChild(document.createTextNode(error.message));
        tr.appendChild(td);
        rows.appendChild(tr);
      });
  }

  var presetForm = document.querySelector('[data-preset-form]');

  function openPreset(p) {
    editingName = p ? p.name : null;
    var dialog = dialogs.preset;
    if (!dialog || !presetForm) return;
    dialog.querySelector('[data-preset-dialog-title]').textContent =
      p ? 'Edit ' + p.name : 'Add preset';
    presetForm.reset();
    if (p) {
      presetForm.querySelector('#preset-name').value = p.name || '';
      presetForm.querySelector('#preset-description').value = p.description || '';
      presetForm.querySelector('#preset-host').value = p.host || '';
      presetForm.querySelector('#preset-port').value = p.port || '';
      presetForm.querySelector('#preset-lu').value = p.luName || '';
      presetForm.querySelector('#preset-model').value = p.model || '';
      presetForm.querySelector('#preset-codepage').value = p.codePage || '';
      presetForm.querySelector('[data-preset-tls]').checked = !!p.tls;
      presetForm.querySelector('[data-preset-skipverify]').checked = !!p.skipVerify;
      presetForm.querySelector('#preset-groups').value = (p.groups || []).join(', ');
      presetForm.querySelector('#preset-users').value = (p.users || []).join(', ');
      presetForm.querySelectorAll('[data-preset-role]').forEach(function (box) {
        box.checked = (p.roles || []).indexOf(box.value) !== -1;
      });
    }
    // After the fields, so the select agrees with the host actually loaded.
    if (sampleSelect) {
      var sample = p ? sampleAppFor(p.host) : null;
      sampleSelect.value = sample ? sample.host : '';
    }
    openDialog('preset');
  }

  var addButton = document.querySelector('[data-preset-add]');
  if (addButton) {
    addButton.addEventListener('click', function () { openPreset(null); });
  }

  if (presetForm) {
    presetForm.addEventListener('submit', function (event) {
      event.preventDefault();
      var roles = [];
      presetForm.querySelectorAll('[data-preset-role]').forEach(function (box) {
        if (box.checked) roles.push(box.value);
      });
      var payload = {
        name: presetForm.querySelector('#preset-name').value.trim(),
        description: presetForm.querySelector('#preset-description').value.trim(),
        host: presetForm.querySelector('#preset-host').value.trim(),
        port: parseInt(presetForm.querySelector('#preset-port').value, 10) || 0,
        luName: presetForm.querySelector('#preset-lu').value.trim(),
        model: presetForm.querySelector('#preset-model').value.trim(),
        codePage: presetForm.querySelector('#preset-codepage').value.trim(),
        tls: presetForm.querySelector('[data-preset-tls]').checked,
        skipVerify: presetForm.querySelector('[data-preset-skipverify]').checked,
        groups: splitList(presetForm.querySelector('#preset-groups').value),
        users: splitList(presetForm.querySelector('#preset-users').value),
        roles: roles
      };

      // Renaming during an edit would otherwise leave the old entry behind:
      // the store upserts by name, so the rename is a save plus a delete.
      var oldName = editingName && editingName !== payload.name ? editingName : null;

      api('POST', '/api/admin/profiles', payload)
        .then(function () {
          if (oldName) {
            return api('POST', '/api/admin/profiles/delete', { name: oldName });
          }
        })
        .then(function () {
          closeDialogs();
          setStatus(payload.name + ' saved.', 'ok');
          load();
        })
        .catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  /* ------------------------------------------------------------- branding */

  function loadBranding() {
    return api('GET', '/api/admin/menu-branding').then(function (data) {
      var brand = data.branding || {};
      document.querySelector('#branding-title-input').value = brand.Title || brand.title || '';
      document.querySelector('#branding-banner').value = (brand.Banner || brand.banner || []).join('\n');
      document.querySelector('#branding-footer').value = brand.Footer || brand.footer || '';
      var limits = data.limits || {};
      document.querySelector('[data-branding-limits]').textContent =
        'Up to ' + limits.bannerLines + ' lines of ' + limits.lineWidth +
        ' characters. ' + limits.charset + '.';
      return refreshPreview();
    });
  }

  function refreshPreview() {
    return api('GET', '/api/admin/menu-preview').then(function (data) {
      document.querySelector('[data-branding-preview]').textContent =
        (data.rows || []).join('\n');
    });
  }

  var brandingForm = document.querySelector('[data-branding-form]');
  if (brandingForm) {
    brandingForm.addEventListener('submit', function (event) {
      event.preventDefault();
      api('POST', '/api/admin/menu-branding', {
        Title: document.querySelector('#branding-title-input').value,
        Banner: document.querySelector('#branding-banner').value.split('\n'),
        Footer: document.querySelector('#branding-footer').value
      }).then(function (data) {
        // What the server changed is shown rather than swallowed: silently
        // dropping a character somebody drew artwork with is how a banner
        // ends up wrong on a screen the person editing it never sees.
        var notes = brandingForm.querySelector('[data-branding-notes]');
        if ((data.notes || []).length) {
          notes.querySelector('span').textContent = data.notes.join('. ') + '.';
          notes.hidden = false;
        } else {
          notes.hidden = true;
        }
        setStatus('Branding saved.', 'ok');
        return refreshPreview();
      }).catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  var brandingReset = document.querySelector('[data-branding-reset]');
  if (brandingReset) {
    brandingReset.addEventListener('click', function () {
      api('POST', '/api/admin/menu-branding', { reset: true })
        .then(function () {
          setStatus('Branding restored to the default.', 'ok');
          return loadBranding();
        })
        .catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  load();
  loadBranding().catch(function (error) { setStatus(error.message, 'error'); });
})();
