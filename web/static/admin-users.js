/* Account administration.
   -------------------------------------------------------------------------
   Renders the account table and drives the add / reset / role / enable /
   delete actions against /api/admin/users.

   The server is the authority on every rule here — last-admin protection,
   self-lockout, password length. This mirrors those rules only to grey out
   controls that would fail, and always shows the server's message when one
   does, rather than inventing its own. */
(function () {
  'use strict';

  var rows = document.querySelector('[data-admin-rows]');
  if (!rows) return;

  var statusEl = document.querySelector('[data-admin-status]');
  var countEl = document.querySelector('[data-admin-count]');
  var dialogs = {};
  var lastFocus = null;
  var cache = [];

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
    // The status line sits above the table, at the top of a page that on a
    // phone is several screens long. Saying "account created" where nobody is
    // looking is the same as not saying it: after adding an account the
    // administrator is left staring at an unchanged form area wondering
    // whether it worked. Bring the answer to them.
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
          // Prefer the server's wording; it knows why it refused.
          throw new Error(data.error || ('Request failed (' + response.status + ')'));
        }
        return data;
      });
    });
  }

  function openDialog(name) {
    var dialog = dialogs[name];
    if (!dialog) return;
    lastFocus = document.activeElement;
    dialog.hidden = false;
    // The first thing to fill in, not the first thing in the markup — the
    // close button leads the panel, and opening a dialog with focus on its
    // dismiss control reads as "are you sure you meant to do that".
    var focusable = dialog.querySelector('input, select') ||
      dialog.querySelector('button:not(.admin-dialog-close)');
    if (focusable) focusable.focus();
    // A dialog re-opened after being scrolled through keeps its old scroll
    // offset, which on a phone means opening onto the middle of the form.
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
      // Backdrop click closes; a click inside the panel must not.
      if (event.target === dialogs[key]) closeDialogs();
    });
  });

  /* --------------------------------------------------------------- render */

  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function pill(className, text) {
    return el('span', 'pill ' + className, text);
  }

  function actionButton(label, className, handler) {
    var button = el('button', className || '', label);
    button.type = 'button';
    button.addEventListener('click', handler);
    return button;
  }

  function renderRow(user) {
    var tr = el('tr');
    tr.setAttribute('data-disabled', String(user.disabled));

    var accountCell = el('td');
    var wrap = el('div', 'admin-user');
    wrap.appendChild(el('span', 'admin-avatar', (user.username || '?').charAt(0)));
    var nameWrap = el('div');
    nameWrap.appendChild(el('div', 'admin-username', user.username));
    if (user.self) nameWrap.appendChild(el('span', 'admin-self-tag', 'you'));
    wrap.appendChild(nameWrap);
    accountCell.appendChild(wrap);
    tr.appendChild(accountCell);

    var roleCell = el('td');
    roleCell.appendChild(user.role === 'admin'
      ? pill('pill--admin', 'Administrator')
      : pill('pill--user', 'User'));
    tr.appendChild(roleCell);

    var statusCell = el('td');
    if (user.disabled) {
      statusCell.appendChild(pill('pill--off', 'Disabled'));
    } else if (user.mustChangePassword) {
      statusCell.appendChild(pill('pill--warn', 'Must change password'));
    } else {
      statusCell.appendChild(pill('pill--ok', 'Active'));
    }
    tr.appendChild(statusCell);

    tr.appendChild(el('td', '', user.createdAt));

    var actionsCell = el('td', 'admin-actions-cell');
    var actions = el('div', 'admin-actions');

    actions.appendChild(actionButton('Reset password', '', function () {
      openReset(user);
    }));

    // Each of these would lock the administrator out of the page they are
    // standing on, so the server refuses them and the UI does not offer them.
    if (!user.self) {
      actions.appendChild(actionButton(
        user.role === 'admin' ? 'Make user' : 'Make admin', '',
        function () {
          confirmThen(
            user.role === 'admin' ? 'Remove administrator role?' : 'Grant administrator role?',
            user.role === 'admin'
              ? user.username + ' will keep their own sessions but lose account management.'
              : user.username + ' will be able to manage accounts and instance settings.',
            function () {
              update(user, { role: user.role === 'admin' ? 'user' : 'admin' },
                'Role updated for ' + user.username + '.');
            });
        }));

      actions.appendChild(actionButton(
        user.disabled ? 'Enable' : 'Disable', '',
        function () {
          if (!user.disabled) {
            confirmThen('Disable ' + user.username + '?',
              'They will be signed out immediately and will not be able to sign in again until re-enabled.',
              function () {
                update(user, { disabled: true }, user.username + ' disabled.');
              });
          } else {
            update(user, { disabled: false }, user.username + ' enabled.');
          }
        }));

      actions.appendChild(actionButton('Delete', 'danger', function () {
        confirmThen('Delete ' + user.username + '?',
          'The account is removed permanently and they are signed out. This cannot be undone.',
          function () {
            api('DELETE', '/api/admin/users/' + encodeURIComponent(user.id))
              .then(function () {
                setStatus(user.username + ' deleted.', 'ok');
                load();
              })
              .catch(function (error) { setStatus(error.message, 'error'); });
          });
      }));
    }

    actionsCell.appendChild(actions);
    tr.appendChild(actionsCell);
    return tr;
  }

  function render(users) {
    cache = users;
    rows.textContent = '';

    if (!users.length) {
      var tr = el('tr');
      var td = el('td', 'admin-empty');
      td.colSpan = 5;
      td.appendChild(el('strong', '', 'No accounts yet'));
      td.appendChild(document.createTextNode('Add one to let somebody sign in.'));
      tr.appendChild(td);
      rows.appendChild(tr);
    } else {
      users.forEach(function (user) { rows.appendChild(renderRow(user)); });
    }

    if (countEl) {
      var admins = users.filter(function (u) { return u.role === 'admin'; }).length;
      countEl.textContent = users.length + (users.length === 1 ? ' account' : ' accounts') +
        ' · ' + admins + (admins === 1 ? ' administrator' : ' administrators');
    }
  }

  /* --------------------------------------------------------------- actions */

  function load() {
    return api('GET', '/api/admin/users')
      .then(function (data) { render(data.users || []); })
      .catch(function (error) {
        rows.textContent = '';
        var tr = el('tr');
        var td = el('td', 'admin-empty');
        td.colSpan = 5;
        td.appendChild(el('strong', '', 'Could not load accounts'));
        td.appendChild(document.createTextNode(error.message));
        tr.appendChild(td);
        rows.appendChild(tr);
        if (countEl) countEl.textContent = '';
      });
  }

  function update(user, patch, successMessage) {
    return api('PATCH', '/api/admin/users/' + encodeURIComponent(user.id), patch)
      .then(function () {
        setStatus(successMessage, 'ok');
        return load();
      })
      .catch(function (error) { setStatus(error.message, 'error'); });
  }

  function confirmThen(title, description, action) {
    var dialog = dialogs.confirm;
    if (!dialog) { action(); return; }

    dialog.querySelector('[data-confirm-title]').textContent = title;
    dialog.querySelector('[data-confirm-desc]').textContent = description;

    var accept = dialog.querySelector('[data-confirm-accept]');
    // Replacing the node drops every listener from a previous confirmation,
    // so an old action cannot fire on this one.
    var fresh = accept.cloneNode(true);
    accept.parentNode.replaceChild(fresh, accept);
    fresh.addEventListener('click', function () {
      closeDialogs();
      action();
    });

    openDialog('confirm');
  }

  var resetTarget = null;

  function openReset(user) {
    resetTarget = user;
    var dialog = dialogs.reset;
    if (!dialog) return;
    dialog.querySelector('[data-reset-name]').textContent = user.username;
    var form = dialog.querySelector('[data-reset-form]');
    form.reset();
    var meter = form.querySelector('[data-pw-meter]');
    if (meter) meter.setAttribute('data-score', '0');
    openDialog('reset');
  }

  var addButton = document.querySelector('[data-admin-add]');
  if (addButton) {
    addButton.addEventListener('click', function () {
      var form = document.querySelector('[data-add-form]');
      if (form) {
        form.reset();
        var meter = form.querySelector('[data-pw-meter]');
        if (meter) meter.setAttribute('data-score', '0');
      }
      openDialog('add');
    });
  }

  var refreshButton = document.querySelector('[data-admin-refresh]');
  if (refreshButton) {
    refreshButton.addEventListener('click', function () {
      setStatus('');
      load();
    });
  }

  var addForm = document.querySelector('[data-add-form]');
  if (addForm) {
    addForm.addEventListener('submit', function (event) {
      event.preventDefault();
      var data = new FormData(addForm);
      api('POST', '/api/admin/users', {
        username: (data.get('username') || '').trim(),
        password: data.get('password') || '',
        role: data.get('role') || 'user'
      }).then(function (result) {
        closeDialogs();
        var name = result.user ? result.user.username : 'Account';
        setStatus(name + ' created. They will choose their own password at first sign-in.', 'ok');
        load();
      }).catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  var resetForm = document.querySelector('[data-reset-form]');
  if (resetForm) {
    resetForm.addEventListener('submit', function (event) {
      event.preventDefault();
      if (!resetTarget) return;
      var data = new FormData(resetForm);
      var target = resetTarget;
      api('PATCH', '/api/admin/users/' + encodeURIComponent(target.id), {
        password: data.get('password') || ''
      }).then(function () {
        closeDialogs();
        setStatus('Password reset for ' + target.username + '. They have been signed out.', 'ok');
        load();
      }).catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  load();
})();
