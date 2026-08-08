/* Shared behaviour for the sign-in, setup, password-change and account
   dialogs.
   -------------------------------------------------------------------------
   Everything here is progressive: the forms are ordinary HTML that submit and
   validate server-side on their own. This only adds the feedback that makes
   choosing a password feel less like guessing what the server will accept. */
(function () {
  'use strict';

  var MIN_LENGTH = parseInt(document.body.getAttribute('data-min-password-length'), 10) || 12;

  /* Score a password 0-4 for the strength meter.
     This is a rough signal to encourage length and variety, not a security
     control — the server enforces the only rule that actually gates anything,
     which is the length floor. */
  function score(password) {
    if (!password) return 0;
    var points = 0;
    if (password.length >= MIN_LENGTH) points++;
    if (password.length >= MIN_LENGTH + 6) points++;
    if (password.length >= MIN_LENGTH + 14) points++;

    var classes = 0;
    if (/[a-z]/.test(password)) classes++;
    if (/[A-Z]/.test(password)) classes++;
    if (/[0-9]/.test(password)) classes++;
    if (/[^A-Za-z0-9]/.test(password)) classes++;
    if (classes >= 3) points++;

    // A long passphrase of one class is genuinely fine; do not punish it.
    if (password.length >= MIN_LENGTH + 10 && points < 3) points = 3;

    return Math.min(points, 4);
  }

  var LABELS = ['', 'Weak', 'Fair', 'Good', 'Strong'];

  function wirePasswordMeter(root) {
    var input = root.querySelector('[data-pw-input]');
    if (!input) return;

    var meter = root.querySelector('[data-pw-meter]');
    var status = root.querySelector('[data-pw-status]');
    var confirm = root.querySelector('[data-pw-confirm]');
    var match = root.querySelector('[data-pw-match]');

    function updateStrength() {
      var value = input.value || '';
      var s = score(value);
      if (meter) meter.setAttribute('data-score', String(s));
      if (!status) return;

      if (!value) {
        status.textContent = 'At least ' + MIN_LENGTH + ' characters.';
        status.removeAttribute('data-state');
      } else if (value.length < MIN_LENGTH) {
        var missing = MIN_LENGTH - value.length;
        status.textContent = missing + ' more character' + (missing === 1 ? '' : 's') + ' needed.';
        status.setAttribute('data-state', 'bad');
      } else {
        status.textContent = LABELS[s] || 'OK';
        status.setAttribute('data-state', 'ok');
      }
    }

    function updateMatch() {
      if (!confirm || !match) return;
      if (!confirm.value) {
        match.textContent = '';
        match.removeAttribute('data-state');
        return;
      }
      if (confirm.value === input.value) {
        match.textContent = 'Passwords match.';
        match.setAttribute('data-state', 'ok');
      } else {
        match.textContent = 'Passwords do not match.';
        match.setAttribute('data-state', 'bad');
      }
    }

    input.addEventListener('input', function () {
      updateStrength();
      updateMatch();
    });
    if (confirm) confirm.addEventListener('input', updateMatch);
    updateStrength();
  }

  /* Stop a double submit from creating two accounts or two sessions. The
     button is only disabled once the browser's own validation has passed, so
     a form rejected client-side stays usable. */
  function wireSubmitGuard(form) {
    form.addEventListener('submit', function () {
      if (form.noValidate === false && !form.checkValidity()) return;
      var button = form.querySelector('button[type="submit"]');
      if (!button) return;
      window.setTimeout(function () {
        button.disabled = true;
        button.dataset.originalLabel = button.textContent;
        button.textContent = 'Working…';
      }, 0);
    });
  }

  /* The setup code is read off a log, so accept whatever shape it arrives in
     and normalise as the operator types. */
  function wireCodeInput(input) {
    input.addEventListener('input', function () {
      var cleaned = input.value.toUpperCase().replace(/[^A-Z2-7]/g, '');
      if (cleaned !== input.value) {
        var atEnd = input.selectionStart === input.value.length;
        input.value = cleaned;
        if (atEnd) input.setSelectionRange(cleaned.length, cleaned.length);
      }
    });
  }

  function init() {
    document.querySelectorAll('[data-auth-form], [data-add-form], [data-reset-form]').forEach(function (form) {
      wirePasswordMeter(form);
      if (form.hasAttribute('data-auth-form')) wireSubmitGuard(form);
    });
    document.querySelectorAll('input.code-input').forEach(wireCodeInput);
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

  // Exposed so the account dialogs can re-run the meter on a freshly opened form.
  window.ThreeSeventyAuthForm = { wirePasswordMeter: wirePasswordMeter };
})();
