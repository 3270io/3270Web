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

  /* ------------------------------------------------------------ generator */

  /* The alphabet leaves out the characters that are the same shape in most
     fonts — O/0, I/l/1 — because these passwords get read aloud, written on
     paper and retyped by somebody else. A generated password nobody can
     transcribe is worse than one they chose themselves. */
  var GEN_ALPHABET = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  var GEN_SYMBOLS = '!@#$%^&*-_=+?';
  /* Comfortably past MIN_LENGTH so a generated password never lands on the
     server's floor and gets rejected for being one short. */
  var GEN_LENGTH = 20;

  /* randomInt returns a uniform value in [0, max) from crypto, rejecting the
     tail of the random range that would otherwise bias the low values —
     Math.random() would do for a throwaway, but this is a real credential. */
  function randomInt(max) {
    var limit = Math.floor(4294967296 / max) * max;
    var buf = new Uint32Array(1);
    var value;
    do {
      window.crypto.getRandomValues(buf);
      value = buf[0];
    } while (value >= limit);
    return value % max;
  }

  function generatePassword() {
    var alphabet = GEN_ALPHABET + GEN_SYMBOLS;
    var out = [];
    var i;
    for (i = 0; i < GEN_LENGTH; i++) {
      out.push(alphabet.charAt(randomInt(alphabet.length)));
    }
    // A uniform draw can leave out a whole class — 20 characters with no digit
    // in them is a plausible hand — and the server's rules, or a site policy
    // layered on later, may want one of each. Plant them rather than hope.
    var required = [
      'abcdefghijkmnopqrstuvwxyz',
      'ABCDEFGHJKLMNPQRSTUVWXYZ',
      '23456789',
      GEN_SYMBOLS,
    ];
    var slots = [];
    while (slots.length < required.length) {
      var slot = randomInt(GEN_LENGTH);
      if (slots.indexOf(slot) === -1) slots.push(slot);
    }
    for (i = 0; i < required.length; i++) {
      out[slots[i]] = required[i].charAt(randomInt(required[i].length));
    }
    return out.join('');
  }

  /* wirePasswordTools adds the reveal / generate / copy controls beside a
     password box.

     An administrator setting somebody else's temporary password has to hand it
     over, so the three go together: inventing one is only useful if it can be
     read back and copied. Reveal defaults to off and the box goes back to dots
     when the dialog is reused. */
  function wirePasswordTools(root) {
    var input = root.querySelector('[data-pw-input]');
    if (!input) return;

    var reveal = root.querySelector('[data-pw-reveal]');
    var generate = root.querySelector('[data-pw-generate]');
    var copy = root.querySelector('[data-pw-copy]');
    var note = root.querySelector('[data-pw-note]');

    function say(text) {
      if (!note) return;
      note.textContent = text || '';
      note.hidden = !text;
    }

    function setRevealed(on) {
      if (!reveal) return;
      input.type = on ? 'text' : 'password';
      reveal.setAttribute('aria-pressed', on ? 'true' : 'false');
      reveal.setAttribute('aria-label', on ? 'Hide password' : 'Show password');
    }

    if (reveal) {
      setRevealed(false);
      reveal.addEventListener('click', function () {
        setRevealed(input.type === 'password');
      });
    }

    if (generate) {
      generate.addEventListener('click', function () {
        if (!window.crypto || !window.crypto.getRandomValues) {
          say('This browser cannot generate a password securely.');
          return;
        }
        input.value = generatePassword();
        // Shown by default: a password the administrator cannot see is one
        // they cannot pass on, which is the whole point of generating it.
        setRevealed(true);
        input.dispatchEvent(new Event('input', { bubbles: true }));
        say('Generated. Copy it now — it is not shown again after saving.');
      });
    }

    if (copy) {
      copy.addEventListener('click', function () {
        var value = input.value || '';
        if (!value) { say('Nothing to copy yet.'); return; }
        var done = function () { say('Copied to the clipboard.'); };
        var failed = function () { say('Could not copy — select the box and copy manually.'); };
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(value).then(done, failed);
          return;
        }
        // Clipboard API needs a secure context; a plain-HTTP instance on a LAN
        // is a normal way to run this, so fall back rather than refuse.
        try {
          var was = input.type;
          input.type = 'text';
          input.select();
          if (document.execCommand('copy')) { done(); } else { failed(); }
          input.type = was;
          setRevealed(input.type !== 'password');
        } catch (_) {
          failed();
        }
      });
    }

    // Re-opening a dialog resets the form; put the tools back to their
    // starting state with it.
    var form = input.form;
    if (form) {
      form.addEventListener('reset', function () {
        window.setTimeout(function () { setRevealed(false); say(''); }, 0);
      });
    }
  }

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
      wirePasswordTools(form);
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
  window.ThreeSeventyAuthForm = {
    wirePasswordMeter: wirePasswordMeter,
    wirePasswordTools: wirePasswordTools,
    generatePassword: generatePassword,
  };
})();
