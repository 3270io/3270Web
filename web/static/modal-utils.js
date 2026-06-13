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

  if (!window.ThreeSeventyWeb) {
    window.ThreeSeventyWeb = {};
  }
  window.ThreeSeventyWeb.createFocusTrap = createFocusTrap;
  window.ThreeSeventyWeb.getFocusable = getFocusable;
})();
