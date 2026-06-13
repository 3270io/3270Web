/**
 * 3270Web notification system.
 * Exposes window.ThreeSeventyWeb.notify(message, type, options)
 * type: 'success' | 'error' | 'info' | 'warning'
 */
(function () {
  'use strict';

  var CONTAINER_ID = 'h3270-notification-container';
  var AUTO_DISMISS_MS = 4500;
  var MAX_VISIBLE = 5;

  function getOrCreateContainer() {
    var existing = document.getElementById(CONTAINER_ID);
    if (existing) {
      return existing;
    }
    var container = document.createElement('div');
    container.id = CONTAINER_ID;
    container.setAttribute('aria-live', 'polite');
    container.setAttribute('aria-atomic', 'false');
    container.setAttribute('role', 'region');
    container.setAttribute('aria-label', 'Notifications');
    document.body.appendChild(container);
    return container;
  }

  function iconForType(type) {
    if (type === 'success') {
      return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>';
    }
    if (type === 'error') {
      return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z"/></svg>';
    }
    if (type === 'warning') {
      return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M1 21h22L12 2 1 21zm12-3h-2v-2h2v2zm0-4h-2v-4h2v4z"/></svg>';
    }
    // info
    return '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z"/></svg>';
  }

  function notify(message, type, options) {
    if (!message) {
      return;
    }
    var resolvedType = type || 'info';
    var opts = options || {};
    // Errors and warnings stay until dismissed so the user can read/act on
    // them; transient success/info toasts auto-dismiss. An explicit
    // opts.duration always wins (use 0 to make any toast persistent).
    var defaultDuration =
      resolvedType === 'error' || resolvedType === 'warning' ? 0 : AUTO_DISMISS_MS;
    var duration = typeof opts.duration === 'number' ? opts.duration : defaultDuration;

    var container = getOrCreateContainer();

    // Limit visible notifications
    var existing = container.querySelectorAll('.h3270-notification');
    if (existing.length >= MAX_VISIBLE) {
      var oldest = existing[0];
      dismissNotification(oldest);
    }

    var toast = document.createElement('div');
    toast.className = 'h3270-notification h3270-notification--' + resolvedType;
    // The container is already an aria-live region, so the toast itself must
    // NOT also be a live region — nesting live regions causes screen readers
    // to announce the message twice.

    var iconWrap = document.createElement('span');
    iconWrap.className = 'h3270-notification__icon';
    iconWrap.innerHTML = iconForType(resolvedType);

    var msgEl = document.createElement('span');
    msgEl.className = 'h3270-notification__message';
    msgEl.textContent = message;

    var closeBtn = document.createElement('button');
    closeBtn.type = 'button';
    closeBtn.className = 'h3270-notification__close';
    closeBtn.setAttribute('aria-label', 'Dismiss notification');
    closeBtn.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M18 6L6 18M6 6l12 12" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>';

    toast.appendChild(iconWrap);
    toast.appendChild(msgEl);
    toast.appendChild(closeBtn);
    container.appendChild(toast);

    // Trigger entrance animation on next frame
    requestAnimationFrame(function () {
      requestAnimationFrame(function () {
        toast.classList.add('h3270-notification--visible');
      });
    });

    var timer = null;

    function dismiss() {
      dismissNotification(toast);
    }

    closeBtn.addEventListener('click', function () {
      clearTimeout(timer);
      dismiss();
    });

    // Pause auto-dismiss on hover
    toast.addEventListener('mouseenter', function () {
      clearTimeout(timer);
    });
    toast.addEventListener('mouseleave', function () {
      if (duration > 0) {
        timer = setTimeout(dismiss, duration);
      }
    });

    if (duration > 0) {
      timer = setTimeout(dismiss, duration);
    }

    return toast;
  }

  function dismissNotification(toast) {
    if (!toast || toast.classList.contains('h3270-notification--leaving')) {
      return;
    }
    toast.classList.remove('h3270-notification--visible');
    toast.classList.add('h3270-notification--leaving');
    toast.addEventListener('transitionend', function handler() {
      toast.removeEventListener('transitionend', handler);
      if (toast.parentNode) {
        toast.parentNode.removeChild(toast);
      }
    });
    // Fallback removal in case transitionend never fires
    setTimeout(function () {
      if (toast.parentNode) {
        toast.parentNode.removeChild(toast);
      }
    }, 600);
  }

  // Attach to global namespace used by the app
  if (!window.ThreeSeventyWeb) {
    window.ThreeSeventyWeb = {};
  }
  window.ThreeSeventyWeb.notify = notify;
})();
