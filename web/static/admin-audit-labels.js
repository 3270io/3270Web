/* How an audit entry is named on screen.
   -------------------------------------------------------------------------
   The overview's recent activity and the audit trail both show the same
   entries, and each used to carry its own copy of this table, which is how the
   two came to disagree about which events had a readable name. This is the one
   copy.

   Event names are stable identifiers, not prose. LABELS gives the readable
   form of what happened; an event nobody has named yet falls back to its raw
   name rather than being hidden, so a newly added one still shows up.

   The outcome matters as much as the event. A setup attempt that was refused
   is recorded under the same event as one that created the administrator and
   differs only in its outcome, so reading the event alone says the opposite of
   what happened. An entry whose outcome is a refusal or a failure is named for
   that, and is marked with a symbol as well as the colour the stylesheet
   gives it. */
(function () {
  'use strict';

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
    'group.role_changed': 'Group role changed',
    'group.created': 'Group created',
    'group.updated': 'Group changed',
    'group.deleted': 'Group deleted',
    'profile.published': 'Host preset published',
    'profile.removed': 'Host preset removed',
    'library.exported': 'Library exported',
    'library.imported': 'Library imported',
    'token.issued': 'API token issued',
    'token.revoked': 'API token revoked',
    'token.refused': 'API token refused',
    'session.opened': 'Session opened',
    'session.denied': 'Session refused',
    'session.closed': 'Session disconnected',
    'printer.started': 'Printer session started',
    'printer.stopped': 'Printer session stopped',
    'file.transfer': 'File transfer',
    'settings.saved': 'Settings changed',
    'app.restarted': 'Server restarted',
    'logs.access_changed': 'Log access changed'
  };

  /* The wording for an event that did not happen, where "created" plus a
     suffix would not read as a sentence. Anything not listed is the event's
     own label with the outcome added; see label(). */
  var NOT_DONE = {
    'account.first_admin_created': 'First administrator setup refused',
    'library.imported': 'Library import failed',
    'file.transfer': 'File transfer failed'
  };

  function isRefusal(entry) {
    return !!entry && (entry.outcome === 'denied' || entry.outcome === 'failure');
  }

  function label(entry) {
    var event = entry && entry.event;
    var base = LABELS[event] || event || '';
    if (!isRefusal(entry)) return base;
    if (NOT_DONE[event]) return NOT_DONE[event];
    // Some labels already say so ("Sign-in failed", "Session refused"), and
    // adding the outcome to them would say it twice.
    if (/\b(refused|failed|throttled|denied)\b/i.test(base)) return base;
    return base + (entry.outcome === 'denied' ? ' (refused)' : ' (failed)');
  }

  /* The pill shown in both views. The symbol is decoration: the wording above
     is what carries the meaning, so a screen reader is not read it twice. */
  function badge(entry) {
    var node = document.createElement('span');
    node.className = 'audit-event';
    node.setAttribute('data-outcome', (entry && entry.outcome) || 'success');
    if (isRefusal(entry)) {
      var icon = document.createElement('span');
      icon.className = 'audit-event-icon';
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = '!';
      node.appendChild(icon);
    }
    node.appendChild(document.createTextNode(label(entry)));
    return node;
  }

  if (!window.ThreeSeventyWeb) window.ThreeSeventyWeb = {};
  window.ThreeSeventyWeb.auditEvent = { label: label, badge: badge, isRefusal: isRefusal };
})();
