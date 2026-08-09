/**
 * 3270Web shared modal helpers.
 *
 * pushModal(modalEl, closeFn) on open and popModal(modalEl) on close are the
 * whole interface. Between them they own the five things every dialog in the
 * app needs and that each one used to arrange for itself:
 *
 *   1. Escape closes the topmost dialog, and only that one.
 *   2. Focus moves into the dialog when it opens.
 *   3. Tab stays inside the dialog while it is open.
 *   4. The page behind the backdrop does not scroll.
 *   5. Focus returns to whatever had it before the dialog opened.
 *
 * They used to be five separate jobs done by hand in each module, which is why
 * they were done inconsistently: a dialog that never moved focus inside itself
 * could activate a focus trap and still watch Tab walk off into the page
 * behind, because the trap listens on the dialog and the Tab never reached it.
 * Doing all five at the single point where a dialog announces itself is what
 * makes them impossible to get half-right.
 *
 *   window.ThreeSeventyWeb.pushModal(modalEl, closeModal); // on open
 *   window.ThreeSeventyWeb.popModal(modalEl);              // on close,
 *   // regardless of how the modal was closed (Escape, button, backdrop
 *   // click) — otherwise a stale entry lingers and shadows whatever opens
 *   // next.
 *
 * A module that wants to place initial focus itself passes
 * { initialFocus: false } and does it after pushModal; passing an element or a
 * selector picks a specific control instead of the first focusable one.
 *
 * Tab is constrained by one document-level handler that always applies to the
 * dialog on top of the stack, so nesting needs no arrangement between the two
 * modules involved. createFocusTrap(modalEl) stays exported for the few
 * surfaces that are not dialogs on this stack (the connect page's pickers, the
 * AI provider sheet).
 *
 * The chaos exploration modals (ui.js) have their own, more elaborate
 * nested-modal stack with backgrounding/parent-child transitions and are
 * deliberately not migrated onto this shared one; their Escape handler
 * already calls preventDefault(), which this listener respects, so the two
 * systems don't fight over the same keypress.
 */
(function () {
  'use strict';

  var FOCUSABLE_SELECTOR = [
    'a[href]',
    'button:not([disabled])',
    'input:not([disabled])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
  ].join(',');

  function isVisible(el) {
    // offsetParent is null for display:none elements; also guard hidden inputs.
    return !!(el && (el.offsetWidth || el.offsetHeight || el.getClientRects().length));
  }

  function getFocusable(container) {
    var nodes = container.querySelectorAll(FOCUSABLE_SELECTOR);
    var out = [];
    for (var i = 0; i < nodes.length; i++) {
      if (isVisible(nodes[i])) {
        out.push(nodes[i]);
      }
    }
    return out;
  }

  function createFocusTrap(container) {
    if (!container) {
      return { activate: function () {}, deactivate: function () {} };
    }

    function onKeydown(event) {
      if (event.key !== 'Tab') {
        return;
      }
      var focusable = getFocusable(container);
      if (focusable.length === 0) {
        // Nothing to focus inside the dialog — keep focus on the container.
        event.preventDefault();
        return;
      }
      var first = focusable[0];
      var last = focusable[focusable.length - 1];
      var active = document.activeElement;

      if (event.shiftKey) {
        if (active === first || !container.contains(active)) {
          event.preventDefault();
          last.focus();
        }
      } else if (active === last || !container.contains(active)) {
        event.preventDefault();
        first.focus();
      }
    }

    return {
      activate: function () {
        container.addEventListener('keydown', onKeydown);
      },
      deactivate: function () {
        container.removeEventListener('keydown', onKeydown);
      },
    };
  }

  /* ------------------------------------------------------------------ */
  /* Background scroll lock                                              */
  /* ------------------------------------------------------------------ */
  /* Counted, because dialogs nest: a confirmation inside a settings panel
     that unlocked the page on its own close would leave the panel behind it
     scrolling. Both <html> and <body> are pinned — which of the two actually
     scrolls depends on the page, and locking only <body> (as each modal used
     to) left the page behind the backdrop scrolling on the terminal screen,
     where <html> is the scroller. */
  var scrollLocks = 0;
  var savedOverflow = null;

  function lockScroll() {
    scrollLocks++;
    if (scrollLocks > 1) {
      return;
    }
    var root = document.documentElement;
    savedOverflow = { root: root.style.overflow, body: document.body.style.overflow };
    root.style.overflow = 'hidden';
    document.body.style.overflow = 'hidden';
  }

  function unlockScroll() {
    if (scrollLocks === 0) {
      return;
    }
    scrollLocks--;
    if (scrollLocks > 0) {
      return;
    }
    document.documentElement.style.overflow = savedOverflow ? savedOverflow.root : '';
    document.body.style.overflow = savedOverflow ? savedOverflow.body : '';
    savedOverflow = null;
  }

  /* ------------------------------------------------------------------ */
  /* Initial focus                                                       */
  /* ------------------------------------------------------------------ */
  function resolveInitialFocus(modal, wanted) {
    if (wanted === false) {
      return null;
    }
    if (wanted && typeof wanted === 'string') {
      return modal.querySelector(wanted);
    }
    if (wanted && typeof wanted.focus === 'function') {
      return wanted;
    }
    var declared = modal.querySelector('[data-modal-autofocus]');
    if (declared && isVisible(declared)) {
      return declared;
    }
    var focusable = getFocusable(modal);
    return focusable.length ? focusable[0] : null;
  }

  function focusInto(modal, wanted) {
    // A module that has already put focus where it wants it is left alone.
    if (modal.contains(document.activeElement) && document.activeElement !== document.body) {
      return;
    }
    var target = resolveInitialFocus(modal, wanted);
    if (target) {
      target.focus();
      // focus() on something that cannot take it is silent, and the dialog
      // would open with focus still on the page behind. A caller naming a
      // control that turns out not to be focusable — a backdrop <div> that
      // shares the close hook with the close button, say — should still get a
      // dialog that has the keyboard.
      if (modal.contains(document.activeElement)) {
        return;
      }
      var fallback = getFocusable(modal);
      if (fallback.length) {
        fallback[0].focus();
        return;
      }
    }
    if (wanted === false) {
      return;
    }
    // A dialog with nothing focusable in it still has to take focus, or the
    // screen reader goes on reading the page behind the backdrop.
    if (!modal.hasAttribute('tabindex')) {
      modal.setAttribute('tabindex', '-1');
    }
    modal.focus();
  }

  var modalStack = [];

  function pushModal(modal, closeFn, options) {
    if (!modal || typeof closeFn !== 'function') {
      return;
    }
    var opts = options || {};
    // Re-opening without a prior pop (shouldn't normally happen) replaces
    // rather than duplicates the entry — and must not take a second scroll
    // lock, which would never be released.
    var existing = null;
    modalStack = modalStack.filter(function (entry) {
      if (entry.modal === modal) {
        existing = entry;
        return false;
      }
      return true;
    });
    if (!existing) {
      lockScroll();
    }
    modalStack.push({
      modal: modal,
      close: closeFn,
      returnFocus: existing ? existing.returnFocus : document.activeElement,
    });
    focusInto(modal, opts.initialFocus);
  }

  function popModal(modal) {
    var removed = null;
    modalStack = modalStack.filter(function (entry) {
      if (entry.modal === modal) {
        removed = entry;
        return false;
      }
      return true;
    });
    if (!removed) {
      return;
    }
    unlockScroll();
    var back = removed.returnFocus;
    if (back && typeof back.focus === 'function' && document.contains(back)) {
      back.focus();
    }
  }

  /* One Tab handler for the whole stack, applying to whichever dialog is on
     top. Per-dialog traps cannot express that: a confirmation opened inside a
     larger panel sits inside it in the DOM, so both traps would fire on the
     same Tab and fight over where focus lands — which is why the modules that
     nest dialogs had to hand traps back and forth by hand. */
  document.addEventListener(
    'keydown',
    function (event) {
      if (event.key !== 'Tab' || event.defaultPrevented || modalStack.length === 0) {
        return;
      }
      var container = modalStack[modalStack.length - 1].modal;
      var focusable = getFocusable(container);
      if (focusable.length === 0) {
        event.preventDefault();
        return;
      }
      var first = focusable[0];
      var last = focusable[focusable.length - 1];
      var active = document.activeElement;
      if (event.shiftKey) {
        if (active === first || !container.contains(active)) {
          event.preventDefault();
          last.focus();
        }
      } else if (active === last || !container.contains(active)) {
        event.preventDefault();
        first.focus();
      }
    },
    true
  );

  document.addEventListener('keydown', function (event) {
    if (event.key !== 'Escape' || event.defaultPrevented || modalStack.length === 0) {
      return;
    }
    var top = modalStack[modalStack.length - 1];
    event.preventDefault();
    popModal(top.modal);
    top.close();
  });

  if (!window.ThreeSeventyWeb) {
    window.ThreeSeventyWeb = {};
  }
  window.ThreeSeventyWeb.createFocusTrap = createFocusTrap;
  window.ThreeSeventyWeb.getFocusable = getFocusable;
  window.ThreeSeventyWeb.pushModal = pushModal;
  window.ThreeSeventyWeb.popModal = popModal;
})();
