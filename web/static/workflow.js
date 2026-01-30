(() => {
  const uploadForm = document.querySelector('[data-workflow-upload]');
  if (uploadForm) {
    const fileInput = uploadForm.querySelector('input[type="file"]');
    const trigger = uploadForm.querySelector('[data-workflow-trigger]');
    if (trigger && fileInput) {
      trigger.addEventListener('click', () => {
        fileInput.click();
      });
      fileInput.addEventListener('change', () => {
        if (fileInput.files && fileInput.files.length > 0) {
          const width = trigger.offsetWidth;
          trigger.style.width = `${width}px`;
          if (trigger.classList.contains('icon-button')) {
            trigger.innerHTML =
              '<span class="spinner" aria-hidden="true" style="margin-right: 0"></span>';
            trigger.setAttribute('aria-label', 'Loading...');
          } else {
            trigger.innerHTML =
              '<span class="spinner" aria-hidden="true"></span> Loading...';
          }
          trigger.setAttribute('aria-busy', 'true');
          trigger.disabled = true;

          uploadForm.submit();
        }
      });
    }
  }

  const modal = document.querySelector('[data-modal]');
  if (!modal) {
    return;
  }

  let lastFocusedElement = null;

  const keepInBounds = () => {
    const rect = modal.getBoundingClientRect();
    const maxLeft = Math.max(20, window.innerWidth - rect.width - 20);
    const maxTop = Math.max(20, window.innerHeight - rect.height - 20);
    let nextLeft = rect.left;
    let nextTop = rect.top;
    if (!Number.isFinite(nextLeft) || nextLeft < 0 || nextLeft > maxLeft) {
      nextLeft = Math.min(120, maxLeft);
    }
    if (!Number.isFinite(nextTop) || nextTop < 0 || nextTop > maxTop) {
      nextTop = Math.min(120, maxTop);
    }
    modal.style.left = `${nextLeft}px`;
    modal.style.top = `${nextTop}px`;
  };
  const openButtons = document.querySelectorAll('[data-modal-open]');
  const closeButtons = modal.querySelectorAll('[data-modal-close]');

  const openModal = () => {
    lastFocusedElement = document.activeElement;
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
    const firstFocusable = modal.querySelector(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    );
    if (firstFocusable) {
      firstFocusable.focus();
    }
  };

  const closeModal = () => {
    modal.hidden = true;
    document.body.style.overflow = '';
    if (lastFocusedElement) {
      lastFocusedElement.focus();
      lastFocusedElement = null;
    }
  };

  openButtons.forEach((button) => {
    button.addEventListener('click', openModal);
  });

  const copyButton = modal.querySelector('[data-modal-copy]');
  if (copyButton) {
    copyButton.addEventListener('click', () => {
      const preview = modal.querySelector('.workflow-preview');
      if (!preview) {
        return;
      }
      const text = preview.textContent;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard
          .writeText(text)
          .then(() => {
            if (copyButton._tippy) {
              copyButton._tippy.setContent('Copied!');
              copyButton._tippy.show();
              setTimeout(() => {
                copyButton._tippy.setContent('Copy to clipboard');
              }, 2000);
            }
          })
          .catch((err) => {
            console.error('Failed to copy:', err);
          });
      }
    });
  }

  closeButtons.forEach((button) => {
    button.addEventListener('click', closeModal);
  });

  modal.addEventListener('click', (event) => {
    if (event.target === modal) {
      closeModal();
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !modal.hidden) {
      closeModal();
    }
  });
})();

(() => {
  const body = document.body;
  if (!body) {
    return;
  }
  const root = document.documentElement;
  const openTriggers = document.querySelectorAll('[data-status-open]');
  const statusWidget = document.querySelector('[data-status-widget]');
  const statusWidgetHeader = statusWidget ? statusWidget.querySelector('[data-status-widget-header]') : null;
  const statusWidgetToggle = statusWidget ? statusWidget.querySelector('[data-status-minimize]') : null;
  const statusWidgetMaximize = statusWidget ? statusWidget.querySelector('[data-status-maximize]') : null;
  const trackingToggle = statusWidget ? statusWidget.querySelector('[data-status-tracking-toggle]') : null;
  const trackingDisabledMessage = statusWidget ? statusWidget.querySelector('[data-status-tracking-disabled]') : null;
  const recordingIndicator = document.querySelector('[data-recording-indicator]');
  const recordingStop = document.querySelector('[data-recording-stop]');
  const recordingStart = document.querySelector('[data-recording-start]');
  const recordingStartDisabled = document.querySelector('[data-recording-start-disabled]');
  const playbackIndicator = document.querySelector('[data-playback-indicator]');
  const playbackComplete = document.querySelector('[data-playback-complete]');
  const playbackStepContainer = document.querySelector('[data-playback-step-container]');
  const playbackDebugControls = document.querySelector('[data-playback-debug-controls]');
  const playbackPlayControls = document.querySelector('[data-playback-play-controls]');
  const playbackStatusText = document.querySelector('[data-playback-status-text]');
  const playbackPausedIndicator = document.querySelector('[data-playback-paused-indicator]');
  const playbackPlayingIndicator = document.querySelector('[data-playback-playing-indicator]');
  const playbackPauseButton = document.querySelector('[data-playback-pause-button]');
  const trackingToggleLabel = statusWidget ? statusWidget.querySelector('.workflow-status-tracking-toggle') : null;

  const ensureButtonTooltip = (button) => {
    if (!button || button.hasAttribute('data-tippy-content')) {
      return;
    }
    const aria = button.getAttribute('aria-label');
    const label = aria || button.textContent.trim();
    if (label) {
      button.setAttribute('data-tippy-content', label);
    }
  };

  document.querySelectorAll('button').forEach(ensureButtonTooltip);
  if (trackingToggleLabel && !trackingToggleLabel.hasAttribute('data-tippy-content')) {
    trackingToggleLabel.setAttribute('data-tippy-content', 'Tracking enabled');
  }

  const tooltipTargets = document.querySelectorAll('[data-tippy-content]');

  const widgetLines = statusWidget
    ? {
        step: statusWidget.querySelector('[data-status-step-line]'),
        type: statusWidget.querySelector('[data-status-type-line]'),
        delayRange: statusWidget.querySelector('[data-status-delay-range-line]'),
        delayApplied: statusWidget.querySelector('[data-status-delay-applied-line]'),
        events: statusWidget.querySelector('[data-status-events]'),
      }
    : null;
  const statusIndicators = Array.from(document.querySelectorAll('[data-status-indicator]'));

  const placeholderText = 'Playback has not started yet.';
  let lastActive = body.dataset.playbackActive === 'true';
  let lastPaused = body.dataset.playbackPaused === 'true';
  let lastPayload = null;
  const trackingEnabledKey = 'workflowStatusTrackingEnabled';
  let trackingEnabled = true;

  const setHidden = (el, hidden) => {
    if (el) {
      el.hidden = !!hidden;
    }
  };

  if (window.tippy && tooltipTargets.length > 0) {
    window.tippy(tooltipTargets, {
      delay: [150, 0],
      placement: 'bottom',
    });
  }

  const updateTrackingTooltip = (enabled) => {
    if (!trackingToggleLabel) {
      return;
    }
    const label = enabled ? 'Tracking enabled' : 'Tracking disabled';
    trackingToggleLabel.setAttribute('data-tippy-content', label);
    if (trackingToggleLabel._tippy) {
      trackingToggleLabel._tippy.setContent(label);
    }
  };

  const updatePlaybackControls = (payload) => {
    if (!payload) {
      return;
    }
    const active = !!payload.playbackActive;
    const paused = !!payload.playbackPaused;
    const completed = !!payload.playbackCompleted && !active;
    const mode = payload.playbackMode || '';
    const debugMode = mode === 'debug';
    const recordingActive = recordingIndicator ? !recordingIndicator.hidden : false;

    setHidden(playbackIndicator, !active);
    setHidden(playbackComplete, !completed);
    setHidden(playbackDebugControls, !(active && debugMode));
    setHidden(playbackPlayControls, !(active && !debugMode));
    setHidden(playbackStepContainer, !(payload.playbackStep > 0));

    if (playbackStatusText) {
      playbackStatusText.textContent = debugMode ? 'DEBUG' : paused ? 'PAUSE' : 'PLAY';
    }
    setHidden(playbackPausedIndicator, !(active && !debugMode && paused));
    setHidden(playbackPlayingIndicator, !(active && !debugMode && !paused));
    if (playbackPauseButton) {
      const label = paused ? 'Resume' : 'Pause';
      playbackPauseButton.setAttribute('aria-label', label);
      playbackPauseButton.setAttribute('data-tippy-content', label);
      if (playbackPauseButton._tippy) {
        playbackPauseButton._tippy.setContent(label);
      }
    }

    setHidden(recordingStartDisabled, recordingActive || !active);
    if (recordingStart) {
      setHidden(recordingStart, recordingActive || active);
    }
    if (recordingStop) {
      setHidden(recordingStop, !recordingActive);
    }
  };

  const escapeHtml = (value = '') => {
    const text = value == null ? '' : String(value);
    return text
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  };

  const renderEvents = (events = []) => {
    if (!Array.isArray(events) || events.length === 0) {
      return '';
    }
    return events
      .map((event) => {
        const time = escapeHtml(event.time);
        const message = escapeHtml(event.message);
        return `<div class="workflow-status-event"><span class="workflow-status-time">${time}</span><span>${message}</span></div>`;
      })
      .join('');
  };

  const updateWorkflowStatus = (payload) => {
    if (!payload) {
      return;
    }
    lastPayload = payload;
    lastActive = !!payload.playbackActive;
    lastPaused = !!payload.playbackPaused;
    body.dataset.playbackActive = lastActive ? 'true' : 'false';
    body.dataset.playbackPaused = lastPaused ? 'true' : 'false';
    body.dataset.playbackCompleted = payload.playbackCompleted ? 'true' : 'false';
    updatePlaybackControls(payload);
    if (!trackingEnabled) {
      return;
    }
    const hasStep = typeof payload.playbackStep === 'number' && payload.playbackStep > 0;
    let stepLabel = payload.playbackStepLabel || '';
    if (!stepLabel && hasStep) {
      stepLabel = `Step ${payload.playbackStep}`;
      if (payload.playbackStepTotal && payload.playbackStepTotal > 0) {
        stepLabel = `${stepLabel}/${payload.playbackStepTotal}`;
      }
      if (payload.playbackStepType) {
        stepLabel = `${stepLabel}: ${payload.playbackStepType}`;
      }
    }
    if (!hasStep) {
      stepLabel = placeholderText;
    }
    statusIndicators.forEach((indicator) => {
      indicator.textContent = stepLabel;
    });
    const typeText = payload.playbackStepType ? `Type: ${payload.playbackStepType}` : '';
    const rangeText = payload.playbackDelayRange ? `Delay range: ${payload.playbackDelayRange}` : '';
    const appliedText = payload.playbackDelayApplied ? `Applied delay: ${payload.playbackDelayApplied}` : '';
    const eventsHtml = renderEvents(payload.playbackEvents);
    const applyLines = (target) => {
      if (!target) {
        return;
      }
      if (target.step) {
        target.step.textContent = stepLabel;
      }
      if (target.type) {
        target.type.textContent = typeText;
        target.type.hidden = !payload.playbackStepType;
      }
      if (target.delayRange) {
        target.delayRange.textContent = rangeText;
        target.delayRange.hidden = !payload.playbackDelayRange;
      }
      if (target.delayApplied) {
        target.delayApplied.textContent = appliedText;
        target.delayApplied.hidden = !payload.playbackDelayApplied;
      }
      if (target.events) {
        target.events.innerHTML = eventsHtml;
      }
    };

    applyLines(widgetLines);
    const shouldAutoScroll =
      payload.playbackActive && !payload.playbackPaused && !payload.playbackCompleted;
    if (shouldAutoScroll && widgetLines && widgetLines.events && payload.playbackEvents && payload.playbackEvents.length > 0) {
      if (statusWidget && statusWidget.classList.contains('is-minimized')) {
        return;
      }
      widgetLines.events.scrollTo({ top: widgetLines.events.scrollHeight, behavior: 'smooth' });
    }
  };

  const fetchWorkflowStatus = () => {
    return fetch('/workflow/status', {
      headers: {
        Accept: 'application/json',
        'Cache-Control': 'no-cache',
      },
    })
      .then((res) => {
        if (res.status === 401 || res.status === 403) {
          return null;
        }
        if (!res.ok) {
          return null;
        }
        return res.json();
      })
      .catch(() => {
        // ignore transient errors
        return null;
      });
  };

  const widgetMinimizedKey = 'workflowStatusWidgetMinimized';
  const widgetSizeKey = 'workflowStatusWidgetSize';
  const widgetMaximizedKey = 'workflowStatusWidgetMaximized';

  const applyTrackingState = (enabled) => {
    trackingEnabled = enabled;
    if (trackingToggle) {
      trackingToggle.checked = enabled;
    }
    updateTrackingTooltip(enabled);
    setHidden(trackingDisabledMessage, enabled);
    if (statusWidget) {
      statusWidget.classList.toggle('is-tracking-disabled', !enabled);
    }
    if (enabled && lastPayload) {
      updateWorkflowStatus(lastPayload);
    }
  };

  const restoreTrackingState = () => {
    try {
      const stored = localStorage.getItem(trackingEnabledKey);
      if (stored === null) {
        applyTrackingState(true);
        return;
      }
      applyTrackingState(stored === '1');
    } catch (err) {
      applyTrackingState(true);
    }
  };

  if (trackingToggle) {
    trackingToggle.addEventListener('change', () => {
      applyTrackingState(trackingToggle.checked);
      try {
        localStorage.setItem(trackingEnabledKey, trackingToggle.checked ? '1' : '0');
      } catch (err) {
        // ignore
      }
    });
  }

  let lastSavedSize = null;

  const applyStoredSize = () => {
    if (!statusWidget) {
      return;
    }
    try {
      const size = JSON.parse(localStorage.getItem(widgetSizeKey) || 'null');
      if (size && typeof size.width === 'number' && size.width >= 220) {
        statusWidget.style.width = `${size.width}px`;
      }
      if (size && typeof size.height === 'number' && size.height >= 80) {
        statusWidget.style.height = `${size.height}px`;
      }
      // Initialize lastSavedSize with the restored size if both dimensions are valid
      if (size && typeof size.width === 'number' && typeof size.height === 'number') {
        lastSavedSize = { width: size.width, height: size.height };
      }
    } catch (err) {
      // ignore
    }
  };

  const saveWidgetSize = () => {
    if (!statusWidget || statusWidget.classList.contains('is-minimized') || statusWidget.classList.contains('is-maximized')) {
      return;
    }
    try {
      const size = { width: statusWidget.offsetWidth, height: statusWidget.offsetHeight };
      // Only save if size changed by at least 3px in either dimension
      if (lastSavedSize && 
          Math.abs(size.width - lastSavedSize.width) < 3 && 
          Math.abs(size.height - lastSavedSize.height) < 3) {
        return;
      }
      lastSavedSize = size;
      localStorage.setItem(widgetSizeKey, JSON.stringify(size));
    } catch (err) {
      // ignore
    }
  };

  const setWidgetMinimized = (minimized) => {
    if (!statusWidget) {
      return;
    }
    if (minimized) {
      setWidgetMaximized(false);
    }
    statusWidget.classList.toggle('is-minimized', minimized);
    if (root) {
      root.classList.toggle('workflow-status-minimized', minimized);
      if (minimized) {
        root.classList.remove('workflow-status-maximized');
      }
    }
    if (minimized) {
      saveWidgetSize();
      statusWidget.style.width = '';
      statusWidget.style.height = '';
    } else {
      applyStoredSize();
    }
    if (statusWidgetToggle) {
      const label = minimized ? 'Restore workflow status' : 'Minimize workflow status';
      statusWidgetToggle.setAttribute('aria-expanded', minimized ? 'false' : 'true');
      statusWidgetToggle.setAttribute('aria-label', label);
      statusWidgetToggle.setAttribute('data-tippy-content', label);
      if (statusWidgetToggle._tippy) {
        statusWidgetToggle._tippy.setContent(label);
      }
    }
    try {
      localStorage.setItem(widgetMinimizedKey, minimized ? '1' : '0');
    } catch (err) {
      // ignore
    }
  };

  const setWidgetMaximized = (maximized) => {
    if (!statusWidget) {
      return;
    }
    statusWidget.classList.toggle('is-maximized', maximized);
    if (root) {
      root.classList.toggle('workflow-status-maximized', maximized);
      if (maximized) {
        root.classList.remove('workflow-status-minimized');
      }
    }
    if (maximized) {
      saveWidgetSize();
      statusWidget.style.height = '';
    } else {
      applyStoredSize();
    }
    if (statusWidgetMaximize) {
      const label = maximized ? 'Restore workflow status' : 'Maximize workflow status';
      statusWidgetMaximize.setAttribute('aria-expanded', maximized ? 'true' : 'false');
      statusWidgetMaximize.setAttribute('aria-label', label);
      statusWidgetMaximize.setAttribute('data-tippy-content', label);
      if (statusWidgetMaximize._tippy) {
        statusWidgetMaximize._tippy.setContent(label);
      }
    }
    try {
      localStorage.setItem(widgetMaximizedKey, maximized ? '1' : '0');
    } catch (err) {
      // ignore
    }
  };

  const restoreWidgetState = () => {
    if (!statusWidget) {
      return;
    }
    try {
      applyStoredSize();
      const minimized = localStorage.getItem(widgetMinimizedKey) === '1';
      setWidgetMinimized(minimized);
      const maximized = localStorage.getItem(widgetMaximizedKey) === '1';
      if (!minimized) {
        setWidgetMaximized(maximized);
      }
    } catch (err) {
      // ignore
    }
  };

  openTriggers.forEach((trigger) => {
    trigger.addEventListener('click', () => {
      setWidgetMinimized(false);
    });
    trigger.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        setWidgetMinimized(false);
      }
    });
  });

  if (statusWidgetToggle) {
    statusWidgetToggle.addEventListener('click', () => {
      const minimized = statusWidget && statusWidget.classList.contains('is-minimized');
      setWidgetMinimized(!minimized);
    });
  }

  if (statusWidgetMaximize) {
    statusWidgetMaximize.addEventListener('click', () => {
      const maximized = statusWidget && statusWidget.classList.contains('is-maximized');
      if (statusWidget && statusWidget.classList.contains('is-minimized')) {
        setWidgetMinimized(false);
      }
      setWidgetMaximized(!maximized);
    });
  }

  if (statusWidgetHeader) {
    statusWidgetHeader.addEventListener('click', (event) => {
      if (!statusWidget || !statusWidget.classList.contains('is-minimized')) {
        return;
      }
      if (event.target.closest('button, input, label')) {
        return;
      }
      setWidgetMinimized(false);
    });
  }

  if (typeof ResizeObserver !== 'undefined' && statusWidget) {
    const observer = new ResizeObserver(() => {
      saveWidgetSize();
    });
    observer.observe(statusWidget);
  }

  restoreWidgetState();
  restoreTrackingState();

  let playbackPollTimer = null;
  const playbackFastMs = 700;
  const playbackSlowMs = 2000;

  const shouldSkipScreenUpdate = (container) => {
    if (!container) {
      return true;
    }
    const active = document.activeElement;
    return active && container.contains(active);
  };

  const updateScreenContent = (container) => {
    if (!container || shouldSkipScreenUpdate(container)) {
      return Promise.resolve();
    }
    return fetch('/screen/content', {
      headers: {
        Accept: 'application/json',
        'Cache-Control': 'no-cache',
      },
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((payload) => {
        if (!payload || typeof payload.html !== 'string') {
          return;
        }
        container.innerHTML = payload.html;
        if (typeof window.installKeyHandler === 'function') {
          const form = container.querySelector('form.renderer-form');
          const formId = form ? (form.id || form.getAttribute('name')) : null;
          window.installKeyHandler(formId);
        }
      })
      .catch(() => {
        // ignore transient errors
      });
  };

  const pollPlayback = () => {
    if (document.visibilityState !== 'visible') {
      playbackPollTimer = window.setTimeout(pollPlayback, playbackSlowMs);
      return;
    }
    fetchWorkflowStatus()
      .then((payload) => {
        if (payload) {
          updateWorkflowStatus(payload);
        }
        const isActive = payload && payload.playbackActive;
        const isPaused = payload && payload.playbackPaused;
        const container = document.querySelector('.screen-container');
        if (isActive && !isPaused) {
          return updateScreenContent(container).then(() => true);
        }
        return false;
      })
      .finally(() => {
        const delay = lastActive ? playbackFastMs : playbackSlowMs;
        playbackPollTimer = window.setTimeout(pollPlayback, delay);
      });
  };

  if (playbackPollTimer === null) {
    playbackPollTimer = window.setTimeout(pollPlayback, playbackFastMs);
  }
})();
