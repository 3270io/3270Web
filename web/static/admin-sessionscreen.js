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

    tr.appendChild(el('td', 'sessions-host', p.host + ':' + (p.port || 3270)));
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
      td.appendChild(document.createTextNode(
        'Add one to put a mainframe on the selection screen.'));
      tr.appendChild(td);
      rows.appendChild(tr);
      return;
    }
    profiles.forEach(function (p) { rows.appendChild(renderRow(p)); });
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
        fillDatalist('[data-preset-known-groups]', knownGroups);
        fillDatalist('[data-preset-known-users]', knownUsers);
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
