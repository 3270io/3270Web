/**
 * 3270Web shared modal helpers.
 *
 * Exposes window.ThreeSeventyWeb.createFocusTrap(modalEl) which keeps keyboard
 * focus inside a dialog while it is open. Without this, pressing Tab walks focus
 * into the page behind the backdrop, which keyboard and screen-reader users
 * perceive as the dialog "leaking".
 *
 *   const trap = ThreeSeventyWeb.createFocusTrap(modalEl);
 *   trap.activate();    // on open (after the element is visible)
 *   trap.deactivate();  // on close
 *
 * Also exposes pushModal(modalEl, closeFn) / popModal(modalEl): a shared
 * stack that centralizes Escape handling. Most modals used to each install
 * their own independent document-level Escape listener, so with more than
 * one modal open (e.g. a confirmation dialog nested inside another modal) a
 * single Escape press could fire every listener at once instead of closing
 * just the topmost one — a few modals worked around this with hand-written
 * priority checks that had to be kept in sync by hand as modals were added.
 * Only the most-recently-registered modal's closeFn runs on Escape.
 *
 *   window.ThreeSeventyWeb.pushModal(modalEl, closeModal); // on open
 *   window.ThreeSeventyWeb.popModal(modalEl);              // on close,
 *   // regardless of how the modal was closed (Escape, button, backdrop
 *   // click) — otherwise a stale entry lingers and shadows whatever opens
 *   // next.
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

  var modalStack = [];

  function pushModal(modal, closeFn) {
    if (!modal || typeof closeFn !== 'function') {
      return;
    }
    // Re-opening without a prior pop (shouldn't normally happen) replaces
    // rather than duplicates the entry.
    modalStack = modalStack.filter(function (entry) {
      return entry.modal !== modal;
    });
    modalStack.push({ modal: modal, close: closeFn });
  }

  function popModal(modal) {
    modalStack = modalStack.filter(function (entry) {
      return entry.modal !== modal;
    });
  }

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
