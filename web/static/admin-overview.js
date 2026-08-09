/* The administration overview.
   -------------------------------------------------------------------------
   One page that answers "is everything all right?": account counts, who is
   signed in, the terminal sessions this instance is running, and the last
   few audit lines. Reads /api/admin/overview and /api/admin/sessions, and
   offers exactly one power of its own — disconnecting a session.

   The numbers age, so the page refreshes itself every half minute while it
   is visible. Nothing here polls while the tab is hidden: an unattended
   admin tab asking twice a minute forever is load for nobody's benefit. */
(function () {
  'use strict';

  var statusEl = document.querySelector('[data-overview-status]');
  var sessionRows = document.querySelector('[data-sessions-rows]');
  if (!sessionRows) return;

  var authEnabled = document.body.getAttribute('data-auth-enabled') === 'true';

  /* Event names are stable identifiers; these are the readable forms. An
     unrecognised event falls back to its raw name rather than being hidden. */
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
    'session.closed': 'Session disconnected',
    'file.transfer': 'File transfer',
    'settings.saved': 'Settings changed',
    'app.restarted': 'Server restarted',
    'logs.access_changed': 'Log access changed'
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

  function api(method, url) {
    return fetch(url, {
      method: method,
      headers: { 'X-Requested-With': 'XMLHttpRequest' },
      credentials: 'same-origin'
    }).then(function (response) {
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

  /* "12 s ago" beats an ISO timestamp when the question is "is anybody
     actually at this terminal". Precision decays with age, the way anyone
     reads it: seconds, then minutes, then hours. */
  function agoText(seconds) {
    if (seconds < 10) return 'just now';
    if (seconds < 60) return seconds + ' s ago';
    if (seconds < 3600) return Math.floor(seconds / 60) + ' min ago';
    if (seconds < 172800) return Math.floor(seconds / 3600) + ' h ago';
    return Math.floor(seconds / 86400) + ' d ago';
  }

  function whenText(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return iso || '';
    var now = new Date();
    var sameDay = d.toDateString() === now.toDateString();
    var time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    return sameDay ? time : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) + ' ' + time;
  }

  function uptimeText(startedIso) {
    var started = new Date(startedIso);
    if (isNaN(started.getTime())) return '';
    var s = Math.max(0, Math.floor((Date.now() - started.getTime()) / 1000));
    var d = Math.floor(s / 86400);
    var h = Math.floor((s % 86400) / 3600);
    var m = Math.floor((s % 3600) / 60);
    if (d > 0) return d + ' d ' + h + ' h';
    if (h > 0) return h + ' h ' + m + ' min';
    if (m > 0) return m + ' min';
    return s + ' s';
  }

  /* ---------------------------------------------------------------- stats */

  function setStat(name, value, sub) {
    var valueEl = document.querySelector('[data-stat="' + name + '"]');
    var subEl = document.querySelector('[data-stat-sub="' + name + '"]');
    if (valueEl) valueEl.textContent = value;
    if (subEl) subEl.textContent = sub || '';
  }

  function plural(n, one, many) {
    return n + ' ' + (n === 1 ? one : many);
  }

  function renderOverview(data) {
    var accounts = data.accounts || {};
    var signedIn = data.signedIn || {};
    var sessions = data.sessions || {};
    var audit = data.audit || {};

    if (authEnabled) {
      var accountNotes = [plural(accounts.admins || 0, 'administrator', 'administrators')];
      if (accounts.disabled) accountNotes.push(accounts.disabled + ' disabled');
      if (accounts.mustChange) accountNotes.push(accounts.mustChange + ' must change password');
      setStat('accounts', accounts.total || 0, accountNotes.join(' · '));
      setStat('signedIn', signedIn.logins || 0,
        plural(signedIn.users || 0, 'person', 'people') + ' with a live login');
    } else {
      setStat('accounts', '—', 'authentication is off');
      setStat('signedIn', '—', 'single local operator');
    }

    setStat('sessions', sessions.open || 0,
      sessions.capTotal ? 'of ' + sessions.capTotal + ' the instance allows' : 'no instance limit set');

    var refused = audit.refused24h || 0;
    setStat('refused', refused,
      'of ' + plural(audit.total24h || 0, 'audit event', 'audit events'));
    var refusedTile = document.querySelector('[data-stat-tile="refused"]');
    if (refusedTile) {
      if (refused > 0) {
        refusedTile.setAttribute('data-state', 'warn');
      } else {
        refusedTile.removeAttribute('data-state');
      }
    }

    renderActivity(audit.recent || []);

    var meta = document.querySelector('[data-overview-meta]');
    if (meta) {
      meta.hidden = false;
      meta.querySelector('[data-meta="version"]').textContent = data.version || '';
      meta.querySelector('[data-meta="uptime"]').textContent = uptimeText(data.startedAt);
      meta.querySelector('[data-meta="authMode"]').textContent = data.authMode || 'none';
    }
  }

  /* ------------------------------------------------------------- activity */

  function renderActivity(entries) {
    var list = document.querySelector('[data-overview-activity]');
    if (!list) return;
    list.textContent = '';

    if (!entries.length) {
      list.appendChild(el('li', 'admin-activity-empty', 'Nothing recorded yet.'));
      return;
    }

    entries.forEach(function (entry) {
      var li = el('li');
      var refused = entry.outcome === 'denied' || entry.outcome === 'failure';
      if (refused) li.setAttribute('data-outcome', entry.outcome);

      var timeEl = el('time', '', whenText(entry.time));
      if (entry.time) timeEl.setAttribute('datetime', entry.time);
      li.appendChild(timeEl);

      var badge = el('span', 'audit-event', LABELS[entry.event] || entry.event);
      badge.setAttribute('data-outcome', entry.outcome || 'success');
      li.appendChild(badge);

      var actor = entry.actor || {};
      var who = actor.username || actor.userId || '';
      var text = who;
      if (entry.target) text += (text ? ' · ' : '') + entry.target;
      li.appendChild(el('span', 'admin-activity-text', text || '—'));

      list.appendChild(li);
    });
  }

  /* ------------------------------------------------------------- sessions */

  function renderSessions(data) {
    var sessions = data.sessions || [];
    sessionRows.textContent = '';

    var countEl = document.querySelector('[data-sessions-count]');
    if (countEl) {
      countEl.textContent = plural(sessions.length, 'session', 'sessions') +
        (data.capTotal ? ' · limit ' + data.capTotal : '');
    }

    if (!sessions.length) {
      var tr = el('tr');
      var td = el('td', 'admin-empty');
      td.colSpan = 5;
      td.appendChild(el('strong', '', 'No terminal sessions'));
      td.appendChild(document.createTextNode('Nobody is connected to a mainframe right now.'));
      tr.appendChild(td);
      sessionRows.appendChild(tr);
      return;
    }

    sessions.forEach(function (s) {
      var tr = el('tr');

      var ownerCell = el('td');
      var wrap = el('div', 'admin-user');
      var ownerName = s.owner || s.ownerId || 'unowned';
      wrap.appendChild(el('span', 'admin-avatar', ownerName.charAt(0)));
      var nameWrap = el('div');
      nameWrap.appendChild(el('div', 'admin-username', ownerName));
      // Enough of the ID to correlate with a log line; not styled as a thing
      // to copy, because it is not a thing anybody types anywhere.
      nameWrap.appendChild(el('div', 'admin-session-id', s.id ? s.id.slice(0, 8) : ''));
      wrap.appendChild(nameWrap);
      ownerCell.appendChild(wrap);
      tr.appendChild(ownerCell);

      var hostText = s.host ? (s.port ? s.host + ':' + s.port : s.host) : '—';
      tr.appendChild(el('td', 'sessions-host', hostText));

      var statusCell = el('td');
      statusCell.appendChild(s.connected
        ? pill('pill--ok', 'Connected')
        : pill('pill--off', 'Not connected'));
      if (s.busy) {
        // The idle reaper leaves this session alone — a chaos exploration or
        // workflow playback is running — so a disconnect interrupts real work
        // rather than an abandoned screen. Say so where the button is.
        statusCell.appendChild(document.createTextNode(' '));
        statusCell.appendChild(pill('pill--warn', 'Automation running'));
      }
      tr.appendChild(statusCell);

      tr.appendChild(el('td', 'sessions-when', agoText(s.idleSeconds || 0)));

      var actionsCell = el('td', 'admin-actions-cell');
      var actions = el('div', 'admin-actions');
      var btn = el('button', 'danger', 'Disconnect');
      btn.type = 'button';
      btn.addEventListener('click', function () {
        confirmThen(
          'Disconnect ' + ownerName + '’s session?',
          'The connection to ' + (hostText === '—' ? 'the mainframe' : hostText) +
            ' is dropped immediately.' +
            (s.busy ? ' An automated run is in progress and will be interrupted.' : '') +
            ' Anything unsubmitted on the screen is lost. Their sign-in is not affected.',
          function () {
            api('DELETE', '/api/admin/sessions/' + encodeURIComponent(s.id))
              .then(function () {
                setStatus(ownerName + '’s session was disconnected.', 'ok');
                load();
              })
              .catch(function (error) { setStatus(error.message, 'error'); });
          });
      });
      actions.appendChild(btn);
      actionsCell.appendChild(actions);
      tr.appendChild(actionsCell);

      sessionRows.appendChild(tr);
    });
  }

  /* ------------------------------------------------------------ confirm */

  var confirmDialog = document.querySelector('[data-dialog="confirm"]');
  var lastFocus = null;

  function closeConfirm() {
    if (confirmDialog) confirmDialog.hidden = true;
    if (lastFocus && lastFocus.focus) lastFocus.focus();
    lastFocus = null;
  }

  function confirmThen(title, description, action) {
    if (!confirmDialog) { action(); return; }
    confirmDialog.querySelector('[data-confirm-title]').textContent = title;
    confirmDialog.querySelector('[data-confirm-desc]').textContent = description;

    var accept = confirmDialog.querySelector('[data-confirm-accept]');
    // A fresh node drops the previous confirmation's listener, so an old
    // action cannot fire on this one.
    var fresh = accept.cloneNode(true);
    accept.parentNode.replaceChild(fresh, accept);
    fresh.addEventListener('click', function () {
      closeConfirm();
      action();
    });

    lastFocus = document.activeElement;
    confirmDialog.hidden = false;
    fresh.focus();
  }

  if (confirmDialog) {
    confirmDialog.addEventListener('mousedown', function (event) {
      if (event.target === confirmDialog) closeConfirm();
    });
    confirmDialog.querySelectorAll('[data-dialog-cancel]').forEach(function (button) {
      button.addEventListener('click', closeConfirm);
    });
  }

  document.addEventListener('keydown', function (event) {
    if (event.key === 'Escape') closeConfirm();
  });

  /* ---------------------------------------------------------------- load */

  function load() {
    return Promise.all([
      api('GET', '/api/admin/overview'),
      api('GET', '/api/admin/sessions')
    ]).then(function (results) {
      renderOverview(results[0]);
      renderSessions(results[1]);
    }).catch(function (error) {
      setStatus(error.message, 'error');
    });
  }

  var refreshButton = document.querySelector('[data-overview-refresh]');
  if (refreshButton) {
    refreshButton.addEventListener('click', function () {
      setStatus('');
      load();
    });
  }

  var REFRESH_MS = 30000;
  var timer = null;

  function startTimer() {
    if (timer === null) timer = setInterval(load, REFRESH_MS);
  }

  function stopTimer() {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  }

  document.addEventListener('visibilitychange', function () {
    if (document.hidden) {
      stopTimer();
    } else {
      load();
      startTimer();
    }
  });

  load();
  startTimer();
})();
