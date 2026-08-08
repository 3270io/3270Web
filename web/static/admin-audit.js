/* The audit trail view.
   -------------------------------------------------------------------------
   Reads /api/admin/audit and renders it newest first. Filtering happens here
   rather than on the server: the view holds a few hundred entries, so doing
   it in the browser is instant and needs no round trip — and the whole file
   is a download away for anything larger than this page is for. */
(function () {
  'use strict';

  var rows = document.querySelector('[data-audit-rows]');
  if (!rows) return;

  var statusEl = document.querySelector('[data-audit-status]');
  var countEl = document.querySelector('[data-audit-count]');
  var filterEl = document.querySelector('[data-audit-filter]');
  var onlyEl = document.querySelector('[data-audit-only]');
  var pathEl = document.querySelector('[data-audit-path]');
  var entries = [];

  /* Event names are stable identifiers, not prose. These are the readable
     forms; anything unrecognised falls back to the raw name rather than
     being hidden, so a newly added event still shows up. */
  var LABELS = {
    'login.succeeded': 'Signed in',
    'login.failed': 'Sign-in failed',
    'login.locked_out': 'Sign-in throttled',
    'logout': 'Signed out',
    'account.password_changed': 'Password changed',
    'account.created': 'Account created',
    'account.updated': 'Account changed',
    'account.deleted': 'Account deleted',
    'account.first_admin_created': 'First administrator created',
    'token.issued': 'API token issued',
    'token.revoked': 'API token revoked',
    'token.refused': 'API token refused',
    'session.opened': 'Session opened',
    'session.denied': 'Session refused',
    'file.transfer': 'File transfer',
    'settings.saved': 'Settings changed',
    'app.restarted': 'Server restarted',
    'logs.access_changed': 'Log access changed'
  };

  var GROUPS = {
    'sign-in': ['login.succeeded', 'login.failed', 'login.locked_out', 'logout'],
    'session': ['session.opened', 'session.denied', 'file.transfer'],
    'admin': ['account.created', 'account.updated', 'account.deleted',
              'account.first_admin_created', 'settings.saved', 'app.restarted',
              'logs.access_changed', 'token.issued', 'token.revoked']
  };

  function setStatus(message, state) {
    if (!statusEl) return;
    statusEl.textContent = message || '';
    if (state) {
      statusEl.setAttribute('data-state', state);
    } else {
      statusEl.removeAttribute('data-state');
    }
  }

  function cell(text, className) {
    var td = document.createElement('td');
    if (className) td.className = className;
    td.textContent = text == null ? '' : String(text);
    return td;
  }

  /* Local time with the date only when it is not today: a trail read while
     something is going on is mostly "in the last few minutes", and a full
     timestamp on every row buries the part that differs. */
  function when(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return iso || '';
    var now = new Date();
    var sameDay = d.toDateString() === now.toDateString();
    var time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    return sameDay ? time : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) + ' ' + time;
  }

  function who(entry) {
    var actor = entry.actor || {};
    if (actor.username) {
      return actor.kind === 'api-token' ? actor.username + ' (token)' : actor.username;
    }
    if (actor.userId) return actor.userId;
    // A failed sign-in has no actor — nobody was authenticated. The username
    // that was tried is the target, and the row already shows it.
    return '—';
  }

  function detailText(entry) {
    var detail = entry.detail || {};
    return Object.keys(detail).sort().map(function (key) {
      return key + ': ' + detail[key];
    }).join(', ');
  }

  function matches(entry) {
    var only = onlyEl ? onlyEl.value : '';
    if (only === 'refused') {
      if (entry.outcome !== 'denied' && entry.outcome !== 'failure') return false;
    } else if (only && GROUPS[only] && GROUPS[only].indexOf(entry.event) === -1) {
      return false;
    }

    var needle = (filterEl ? filterEl.value : '').trim().toLowerCase();
    if (!needle) return true;
    var haystack = [
      entry.event, LABELS[entry.event], who(entry), entry.clientIp,
      entry.target, detailText(entry), entry.outcome
    ].join(' ').toLowerCase();
    return haystack.indexOf(needle) !== -1;
  }

  function render() {
    var visible = entries.filter(matches);
    rows.textContent = '';

    if (!visible.length) {
      var empty = document.createElement('tr');
      var td = document.createElement('td');
      td.colSpan = 6;
      td.className = 'admin-empty';
      td.textContent = entries.length
        ? 'Nothing matches that filter.'
        : 'Nothing recorded yet.';
      empty.appendChild(td);
      rows.appendChild(empty);
    } else {
      visible.forEach(function (entry) {
        var tr = document.createElement('tr');
        if (entry.outcome === 'denied' || entry.outcome === 'failure') {
          tr.setAttribute('data-outcome', entry.outcome);
        }
        tr.appendChild(cell(when(entry.time), 'audit-when'));

        var event = document.createElement('td');
        var badge = document.createElement('span');
        badge.className = 'audit-event';
        badge.setAttribute('data-outcome', entry.outcome || 'success');
        badge.textContent = LABELS[entry.event] || entry.event;
        event.appendChild(badge);
        tr.appendChild(event);

        tr.appendChild(cell(who(entry)));
        tr.appendChild(cell(entry.clientIp || '—', 'audit-ip'));
        tr.appendChild(cell(entry.target || '—'));
        tr.appendChild(cell(detailText(entry), 'audit-detail'));
        rows.appendChild(tr);
      });
    }

    if (countEl) {
      countEl.textContent = visible.length === entries.length
        ? entries.length + (entries.length === 1 ? ' entry' : ' entries')
        : visible.length + ' of ' + entries.length + ' entries';
    }
  }

  function load() {
    setStatus('');
    fetch('/api/admin/audit?limit=500', {
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
      credentials: 'same-origin'
    }).then(function (response) {
      return response.json().catch(function () { return {}; }).then(function (data) {
        if (!response.ok) throw new Error(data.error || ('Request failed (' + response.status + ')'));
        return data;
      });
    }).then(function (data) {
      entries = data.entries || [];
      if (pathEl && data.path) pathEl.textContent = data.path;
      render();
    }).catch(function (err) {
      setStatus(err.message, 'error');
      rows.textContent = '';
    });
  }

  if (filterEl) filterEl.addEventListener('input', render);
  if (onlyEl) onlyEl.addEventListener('change', render);
  var refresh = document.querySelector('[data-audit-refresh]');
  if (refresh) refresh.addEventListener('click', load);

  load();
})();
