/* Group administration.
   -------------------------------------------------------------------------
   One row per team, and one dialog that makes a group, names it, fills it and
   points it at mainframes. Everything here talks to /api/admin/groups; the
   server owns every rule — name validity, the last-administrator guard, what
   an identity provider owns — and this only greys out what would fail and
   shows the server's own wording when something does.

   The dialog is deliberately one dialog for create and edit. "Make the group,
   then go and find it, then open it to put people in it" is three steps for
   one intention, and the intention is always the whole thing. */
(function () {
  'use strict';

  var rows = document.querySelector('[data-groups-rows]');
  if (!rows) return;

  var statusEl = document.querySelector('[data-groups-status]');
  var countEl = document.querySelector('[data-groups-count]');
  var filterEl = document.querySelector('[data-groups-filter]');
  var memberFilterEl = document.querySelector('[data-group-member-filter]');
  var dialogs = {};
  var lastFocus = null;

  var cache = [];
  var usernames = [];
  var externalUsernames = [];
  var hosts = [];
  var groupsFromProvider = false;
  // The group being edited, or null while creating one.
  var editing = null;

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

  function pill(className, text) {
    return el('span', 'pill ' + className, text);
  }

  function actionButton(label, className, handler) {
    var button = el('button', className || '', label);
    button.type = 'button';
    button.addEventListener('click', handler);
    return button;
  }

  function sameName(a, b) {
    return String(a || '').trim().toLowerCase() === String(b || '').trim().toLowerCase();
  }

  function has(list, name) {
    return (list || []).some(function (item) { return sameName(item, name); });
  }

  /* --------------------------------------------------------------- dialogs */

  /* Dialogs go on the shared modal stack (modal-utils.js), which owns the five
     things a dialog needs and that this file used to arrange for itself: focus
     moves in, Tab stays in, Escape closes the topmost one, the page behind the
     backdrop stops scrolling, and focus goes back where it came from.

     Doing it by hand got three of the five wrong. Focus was placed but never
     held, so Tab walked straight out of an aria-modal dialog and onto the table
     behind it — on a phone that means the next tap lands on Delete for some
     other account. Escape closed every dialog at once rather than the one on
     top. And nothing locked the background, so a flick past the end of a long
     member list scrolled the page underneath instead. */

  function stack() {
    return window.ThreeSeventyWeb;
  }

  function openDialog(name) {
    var dialog = dialogs[name];
    if (!dialog) return;
    dialog.hidden = false;
    // A dialog re-opened after being scrolled through keeps its old scroll
    // offset, which on a phone means opening onto the middle of the form.
    dialog.scrollTop = 0;
    // The first thing to fill in, not the first thing in the markup — the
    // close button leads the panel, and opening a dialog with focus on its
    // dismiss control reads as "are you sure you meant to do that".
    var focusable = dialog.querySelector('input, select') ||
      dialog.querySelector('button:not(.admin-dialog-close)');
    if (stack() && typeof stack().pushModal === 'function') {
      stack().pushModal(dialog, function () { hideDialog(name); },
        { initialFocus: focusable || false });
      return;
    }
    // modal-utils.js absent (an older cached page, a stripped deployment):
    // place focus at least, which is what this file did before.
    lastFocus = document.activeElement;
    if (focusable) focusable.focus();
  }

  // hideDialog closes one dialog. Escape reaches it through the stack, which
  // has already popped the entry by the time this runs; popModal on a dialog
  // that is no longer on the stack is a no-op, so both routes end up here and
  // neither double-pops.
  function hideDialog(name) {
    var dialog = dialogs[name];
    if (!dialog || dialog.hidden) return;
    dialog.hidden = true;
    if (stack() && typeof stack().popModal === 'function') {
      stack().popModal(dialog);
      return;
    }
    if (lastFocus && lastFocus.focus) lastFocus.focus();
    lastFocus = null;
  }

  function closeDialogs() {
    Object.keys(dialogs).forEach(hideDialog);
  }

  // Escape is the shared stack's, which closes only the dialog on top. This
  // stands in when modal-utils.js is not there — an older cached page, a
  // stripped deployment — because a dialog Escape does not close is a dialog
  // somebody is stuck in.
  document.addEventListener('keydown', function (event) {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    if (stack() && typeof stack().pushModal === 'function') return;
    closeDialogs();
  });

  document.querySelectorAll('[data-dialog-cancel]').forEach(function (button) {
    button.addEventListener('click', function () {
      var dialog = button.closest('[data-dialog]');
      if (dialog) {
        hideDialog(dialog.getAttribute('data-dialog'));
        return;
      }
      closeDialogs();
    });
  });

  Object.keys(dialogs).forEach(function (key) {
    dialogs[key].addEventListener('mousedown', function (event) {
      // Backdrop click closes; a click inside the panel must not.
      if (event.target === dialogs[key]) hideDialog(key);
    });
  });

  function confirmThen(title, description, action) {
    var dialog = dialogs.confirm;
    if (!dialog) { action(); return; }

    dialog.querySelector('[data-confirm-title]').textContent = title;
    dialog.querySelector('[data-confirm-desc]').textContent = description;

    var accept = dialog.querySelector('[data-confirm-accept]');
    // Replacing the node drops every listener from a previous confirmation, so
    // an old action cannot fire on this one.
    var fresh = accept.cloneNode(true);
    accept.parentNode.replaceChild(fresh, accept);
    fresh.addEventListener('click', function () {
      closeDialogs();
      action();
    });

    openDialog('confirm');
  }

  /* --------------------------------------------------------------- render */

  function renderRow(group) {
    var tr = el('tr');

    var nameCell = el('td');
    var nameWrap = el('div');
    nameWrap.appendChild(el('div', 'admin-username', group.name));
    if (group.description) {
      nameWrap.appendChild(el('div', 'admin-role-via', group.description));
    }
    if (!group.declared) {
      // A group nobody declared: it exists because an account carries the name
      // or a directory sent it. Worth saying, because it is the one thing an
      // administrator cannot have chosen and may not recognise.
      var tag = el('span', 'admin-origin-tag', 'in use');
      tag.title = 'This group has no record of its own — it exists because an account or a role assignment names it. Saving it here gives it one.';
      nameWrap.appendChild(tag);
    }
    nameCell.appendChild(nameWrap);
    tr.appendChild(nameCell);

    var roleCell = el('td');
    roleCell.appendChild(group.role === 'admin'
      ? pill('pill--admin', 'Administrator')
      : pill('pill--user', 'No extra role'));
    tr.appendChild(roleCell);

    var membersCell = el('td');
    if (!group.memberCount) {
      membersCell.appendChild(el('span', 'admin-groups-none', 'Empty'));
    } else {
      var memberWrap = el('div', 'admin-groups');
      group.members.forEach(function (name) {
        memberWrap.appendChild(el('span', 'admin-group-tag', name));
      });
      membersCell.appendChild(memberWrap);
    }
    tr.appendChild(membersCell);

    var hostsCell = el('td');
    if (!(group.hosts || []).length) {
      hostsCell.appendChild(el('span', 'admin-groups-none', '—'));
    } else {
      var hostWrap = el('div', 'admin-groups');
      group.hosts.forEach(function (name) {
        hostWrap.appendChild(el('span', 'admin-group-tag admin-tag--user', name));
      });
      hostsCell.appendChild(hostWrap);
    }
    tr.appendChild(hostsCell);

    var actionsCell = el('td', 'admin-actions-cell');
    var actions = el('div', 'admin-actions');
    actions.appendChild(actionButton('Edit', '', function () { openGroup(group); }));
    actions.appendChild(actionButton('Delete', 'danger', function () {
      confirmThen('Delete the group ' + group.name + '?',
        'It is removed from every account in it, from every host preset that offers it, ' +
        'and loses any role it granted. The accounts themselves are not touched.',
        function () {
          api('DELETE', '/api/admin/groups/' + encodeURIComponent(group.name))
            .then(function (data) {
              setStatus(group.name + ' deleted.' + noteSuffix(data.notes), 'ok');
              load();
            })
            .catch(function (error) { setStatus(error.message, 'error'); });
        });
    }));
    actionsCell.appendChild(actions);
    tr.appendChild(actionsCell);

    return tr;
  }

  function matches(group) {
    var needle = (filterEl ? filterEl.value : '').trim().toLowerCase();
    if (!needle) return true;
    var haystack = [
      group.name, group.description, group.role,
      (group.members || []).join(' '), (group.hosts || []).join(' ')
    ].join(' ').toLowerCase();
    return haystack.indexOf(needle) !== -1;
  }

  function render(list) {
    cache = list;
    rows.textContent = '';

    var visible = list.filter(matches);

    if (!visible.length) {
      var tr = el('tr');
      var td = el('td', 'admin-empty');
      td.colSpan = 5;
      if (list.length) {
        td.appendChild(el('strong', '', 'Nothing matches that filter'));
        td.appendChild(document.createTextNode('Clear the search to see every group.'));
      } else {
        td.appendChild(el('strong', '', 'No groups yet'));
        td.appendChild(document.createTextNode(
          'Add one to decide, for a team rather than a person at a time, which mainframes are offered.'));
      }
      tr.appendChild(td);
      rows.appendChild(tr);
    } else {
      visible.forEach(function (group) { rows.appendChild(renderRow(group)); });
    }

    if (countEl) {
      var total = list.length + (list.length === 1 ? ' group' : ' groups');
      countEl.textContent = visible.length === list.length
        ? total
        : visible.length + ' of ' + total;
    }
  }

  /* --------------------------------------------------------------- pickers */

  // A checkbox per account and per preset. Checkboxes rather than a
  // comma-separated field because the whole complaint this page answers is
  // that a group had to be spelled identically in three places by hand.

  // Who is ticked, held here rather than read off the boxes. The member list
  // is filterable, so a name the filter has hidden has no checkbox to read —
  // and a selection that forgets whoever is not currently on screen would
  // silently drop half a team on save.
  var memberSelection = [];

  function renderMembers() {
    var list = document.querySelector('[data-group-members]');
    var empty = document.querySelector('[data-group-members-empty]');
    if (!list) return;
    var needle = (memberFilterEl ? memberFilterEl.value : '').trim().toLowerCase();

    list.textContent = '';
    if (empty) empty.hidden = usernames.length > 0;

    usernames.forEach(function (name) {
      if (needle && name.toLowerCase().indexOf(needle) === -1) return;

      var label = el('label', 'preset-check');
      var box = document.createElement('input');
      box.type = 'checkbox';
      box.value = name;
      box.checked = has(memberSelection, name);

      var external = has(externalUsernames, name);
      if (external && groupsFromProvider) {
        // The server leaves these alone, so the control says so rather than
        // accepting a change that the next sign-in would undo.
        box.disabled = true;
        label.className = 'preset-check preset-check--locked';
      }
      box.addEventListener('change', function () {
        memberSelection = memberSelection.filter(function (item) {
          return !sameName(item, name);
        });
        if (box.checked) memberSelection.push(name);
      });

      label.appendChild(box);
      label.appendChild(document.createTextNode(name));
      if (external) {
        label.appendChild(el('span', 'admin-origin-tag', 'single sign-on'));
      }
      list.appendChild(label);
    });

    if (!list.children.length && usernames.length) {
      list.appendChild(el('p', 'field-hint', 'No account matches that filter.'));
    }
  }

  function renderHosts(selected) {
    var list = document.querySelector('[data-group-hosts]');
    var empty = document.querySelector('[data-group-hosts-empty]');
    if (!list) return;

    list.textContent = '';
    if (empty) empty.hidden = hosts.length > 0;

    hosts.forEach(function (host) {
      var label = el('label', 'preset-check');
      var box = document.createElement('input');
      box.type = 'checkbox';
      box.value = host.name;
      box.setAttribute('data-group-host', '');
      box.checked = has(selected, host.name);
      label.appendChild(box);

      var text = el('span');
      text.appendChild(el('strong', '', host.name));
      // A bundled sample app is named the way it was chosen, not by the
      // "sampleapp:app1:3271" it is addressed as internally. Suppressed when
      // it only repeats the preset's name, which is what happens when the
      // preset was named after the sample app it points at.
      var detail = host.sampleApp || host.target;
      if (detail && !sameName(detail, host.name)) {
        text.appendChild(el('span', 'admin-role-via', detail));
      }
      // A preset that names nobody already reaches this group, and everybody
      // else. Ticking it is a narrowing, not a widening, so say so here rather
      // than let it be discovered by whoever stops seeing the host.
      if (host.everyone && !box.checked) {
        text.appendChild(el('span', 'admin-role-via',
          'Currently offered to everyone — assigning it here narrows it to the groups, users and roles it names.'));
      }
      label.appendChild(text);
      list.appendChild(label);
    });
  }

  function selectedValues(attribute) {
    var out = [];
    document.querySelectorAll('[' + attribute + ']').forEach(function (box) {
      if (box.checked) out.push(box.value);
    });
    return out;
  }

  /* --------------------------------------------------------------- actions */

  function noteSuffix(notes) {
    if (!notes || !notes.length) return '';
    return ' ' + notes.join(' ');
  }

  function load() {
    return api('GET', '/api/admin/groups')
      .then(function (data) {
        usernames = data.usernames || [];
        externalUsernames = data.externalUsernames || [];
        hosts = data.hosts || [];
        groupsFromProvider = !!data.groupsFromProvider;
        render(data.groups || []);
      })
      .catch(function (error) {
        rows.textContent = '';
        var tr = el('tr');
        var td = el('td', 'admin-empty');
        td.colSpan = 5;
        td.appendChild(el('strong', '', 'Could not load groups'));
        td.appendChild(document.createTextNode(error.message));
        tr.appendChild(td);
        rows.appendChild(tr);
        if (countEl) countEl.textContent = '';
      });
  }

  function openGroup(group) {
    editing = group || null;
    var dialog = dialogs.group;
    if (!dialog) return;

    dialog.querySelector('[data-group-dialog-title]').textContent =
      group ? 'Edit ' + group.name : 'Add group';
    dialog.querySelector('#group-name').value = group ? group.name : '';
    dialog.querySelector('#group-description').value = group ? (group.description || '') : '';
    dialog.querySelector('#group-role').value = group && group.role === 'admin' ? 'admin' : 'user';

    var providerNote = dialog.querySelector('[data-group-provider-note]');
    if (providerNote) providerNote.hidden = !groupsFromProvider;

    if (memberFilterEl) memberFilterEl.value = '';
    memberSelection = (group && group.members ? group.members : []).slice();
    renderMembers();
    renderHosts(group ? group.hosts : []);

    openDialog('group');
  }

  var form = document.querySelector('[data-group-form]');
  if (form) {
    form.addEventListener('submit', function (event) {
      event.preventDefault();

      var payload = {
        name: (form.querySelector('#group-name').value || '').trim(),
        description: (form.querySelector('#group-description').value || '').trim(),
        role: form.querySelector('#group-role').value || 'user',
        members: memberSelection.slice(),
        hosts: selectedValues('data-group-host')
      };
      if (!payload.name) {
        setStatus('A group needs a name.', 'error');
        return;
      }

      var request = editing
        ? api('PATCH', '/api/admin/groups/' + encodeURIComponent(editing.name), payload)
        : api('POST', '/api/admin/groups', payload);

      var wasEditing = editing;
      request.then(function (data) {
        closeDialogs();
        var notes = noteSuffix(data.notes) + (data.warning ? ' ' + data.warning : '');
        setStatus((wasEditing ? 'Saved ' : 'Created ') + payload.name + '.' + notes,
          data.warning ? 'warn' : 'ok');
        load();
      }).catch(function (error) { setStatus(error.message, 'error'); });
    });
  }

  var addButton = document.querySelector('[data-groups-add]');
  if (addButton) {
    addButton.addEventListener('click', function () { openGroup(null); });
  }

  var refreshButton = document.querySelector('[data-groups-refresh]');
  if (refreshButton) {
    refreshButton.addEventListener('click', function () {
      setStatus('');
      load();
    });
  }

  if (filterEl) filterEl.addEventListener('input', function () { render(cache); });
  if (memberFilterEl) {
    memberFilterEl.addEventListener('input', function () { renderMembers(); });
  }

  load();
})();
