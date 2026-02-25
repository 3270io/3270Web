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
  const workflowLoadTrigger = document.querySelector('[data-workflow-trigger]');
  const workflowPlayButton = document.querySelector('form[action="/workflow/play"] .icon-button');
  const workflowDebugButton = document.querySelector('form[action="/workflow/debug"] .icon-button');
  const workflowStatusFileIcon = document.querySelector('.workflow-status-file-icon');
  const playbackIndicator = document.querySelector('[data-playback-indicator]');
  const playbackComplete = document.querySelector('[data-playback-complete]');
  const activeRunContainer = document.querySelector('[data-active-run-container]');
  const activeRunRow = document.querySelector('[data-active-run-row]');
  const activeRunChip = document.querySelector('[data-active-run-chip]');
  const activeRunMeta = document.querySelector('[data-active-run-meta]');
  const activeRunExtend = document.querySelector('[data-active-run-extend]');
  const activeRunClose = document.querySelector('[data-active-run-close]');
  const chaosExtendButton = document.querySelector('[data-chaos-extend]');
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

  const placeholderText = 'Playback has not started yet.';
  let lastActive = body.dataset.playbackActive === 'true';
  let lastChaosActive = false;
  let lastPaused = body.dataset.playbackPaused === 'true';
  let lastPayload = null;
  let lastRecordingActive = false;
  let lastPlaybackCompleted = body.dataset.playbackCompleted === 'true';
  let lastLoadedWorkflowStepTotal = 0;
  let dismissedActiveRunKey = '';
  const trackingEnabledKey = 'workflowStatusTrackingEnabled';
  let trackingEnabled = true;

  const setHidden = (el, hidden) => {
    if (el) {
      el.hidden = !!hidden;
    }
  };

  const hideActiveRunRow = () => {
    setHidden(activeRunExtend, true);
    setHidden(activeRunClose, true);
    setHidden(activeRunRow, true);
    setHidden(activeRunContainer, true);
  };

  const defaultButtonTooltip = (button) => {
    if (!button) {
      return '';
    }
    if (!Object.prototype.hasOwnProperty.call(button.dataset, 'defaultTooltip')) {
      button.dataset.defaultTooltip = button.getAttribute('data-tippy-content') || '';
    }
    return button.dataset.defaultTooltip;
  };

  const setButtonDisabledState = (button, disabled, disabledTooltip) => {
    if (!button) {
      return;
    }
    if (button.getAttribute('aria-busy') === 'true') {
      return;
    }
    button.disabled = !!disabled;
    const fallback = defaultButtonTooltip(button);
    const tooltip = disabled && disabledTooltip ? disabledTooltip : fallback;
    if (tooltip) {
      button.setAttribute('data-tippy-content', tooltip);
      if (button._tippy) {
        button._tippy.setContent(tooltip);
      }
    }
  };

  const setTooltipContent = (el, content) => {
    if (!el || !content) {
      return;
    }
    el.setAttribute('data-tippy-content', content);
    if (el._tippy) {
      el._tippy.setContent(content);
    }
  };

  const updateLoadedWorkflowTooltips = (payload) => {
    const stepTotal = Number((payload && (payload.loadedWorkflowStepTotal || payload.playbackStepTotal)) || 0);
    const stepSuffix = stepTotal > 0 ? ` (Steps ${stepTotal})` : '';
    if (workflowStatusFileIcon) {
      if (!Object.prototype.hasOwnProperty.call(workflowStatusFileIcon.dataset, 'baseTooltip')) {
        workflowStatusFileIcon.dataset.baseTooltip = workflowStatusFileIcon.getAttribute('data-tippy-content') || '';
      }
      const base = workflowStatusFileIcon.dataset.baseTooltip || '';
      if (base) {
        setTooltipContent(workflowStatusFileIcon, `${base}${stepSuffix}`);
      }
    }
    if (workflowPlayButton && !workflowPlayButton.disabled) {
      const base = defaultButtonTooltip(workflowPlayButton);
      if (base) {
        setTooltipContent(workflowPlayButton, `${base}${stepSuffix}`);
      }
    }
    if (workflowDebugButton && !workflowDebugButton.disabled) {
      const base = defaultButtonTooltip(workflowDebugButton);
      if (base) {
        setTooltipContent(workflowDebugButton, `${base}${stepSuffix}`);
      }
    }
  };

  if (window.tippy && tooltipTargets.length > 0) {
    tooltipTargets.forEach((target) => {
      if (target.hasAttribute('title')) {
        target.removeAttribute('title');
      }
    });
    window.tippy(tooltipTargets, {
      delay: [150, 0],
      placement: 'bottom',
      trigger: 'mouseenter',
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
    const recordingActive = !!payload.recordingActive;
    const blockWorkflowActions = recordingActive || active;
    const blockedTooltip = 'Unavailable while recording or playback is active';

    setHidden(recordingIndicator, !recordingActive);
    setHidden(playbackIndicator, !active);
    setHidden(playbackComplete, !completed);
    setHidden(playbackDebugControls, !(active && debugMode));
    setHidden(playbackPlayControls, !(active && !debugMode));

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
    setButtonDisabledState(workflowLoadTrigger, blockWorkflowActions, blockedTooltip);
    setButtonDisabledState(workflowPlayButton, blockWorkflowActions, blockedTooltip);
    setButtonDisabledState(workflowDebugButton, blockWorkflowActions, blockedTooltip);
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

  const formatStoppedAt = (value) => {
    if (!value) {
      return '';
    }
    const dt = new Date(value);
    if (Number.isNaN(dt.getTime())) {
      return '';
    }
    return dt.toLocaleTimeString();
  };

  const formatElapsed = (startValue, endValue) => {
    if (!startValue) {
      return '';
    }
    const start = new Date(startValue);
    if (Number.isNaN(start.getTime())) {
      return '';
    }
    const end = endValue ? new Date(endValue) : new Date();
    if (Number.isNaN(end.getTime())) {
      return '';
    }
    const deltaSec = Math.max(0, Math.floor((end.getTime() - start.getTime()) / 1000));
    const hours = Math.floor(deltaSec / 3600);
    const minutes = Math.floor((deltaSec % 3600) / 60);
    const seconds = deltaSec % 60;
    if (hours > 0) {
      return `${hours}h ${minutes}m`;
    }
    if (minutes > 0) {
      return `${minutes}m ${seconds}s`;
    }
    return `${seconds}s`;
  };

  const formatDurationMsCompact = (ms) => {
    const totalSec = Math.max(0, Math.floor((Number(ms) || 0) / 1000));
    const hours = Math.floor(totalSec / 3600);
    const minutes = Math.floor((totalSec % 3600) / 60);
    const seconds = totalSec % 60;
    if (hours > 0) {
      return `${hours}h ${minutes}m`;
    }
    if (minutes > 0) {
      return `${minutes}m ${seconds}s`;
    }
    return `${seconds}s`;
  };

  const truncateText = (text, maxChars) => {
    const raw = text == null ? '' : String(text).trim();
    if (!raw || raw.length <= maxChars) {
      return raw;
    }
    return `${raw.slice(0, Math.max(0, maxChars - 1)).trimEnd()}…`;
  };

  const joinParts = (parts) => parts.filter(Boolean).join(' · ');

  const activeRunDismissKeyForPayload = (payload, mode) => {
    if (!payload || !mode) {
      return '';
    }
    if (mode === 'recording') {
      return `recording:${payload.recordingStartedAt || ''}`;
    }
    if (mode === 'playback') {
      return `playback:${payload.playbackStartedAt || ''}:${payload.playbackMode || ''}:${payload.playbackCompleted ? '1' : '0'}`;
    }
    if (mode === 'chaos') {
      return `chaos:${payload.chaosLoadedRunID || ''}:${payload.chaosStartedAt || ''}:${payload.chaosStoppedAt || ''}`;
    }
    return mode;
  };

  const updateActiveRunRow = (payload) => {
    if (!activeRunRow || !activeRunChip || !activeRunMeta || !payload) {
      return;
    }
    const recordingActive = !!payload.recordingActive;
    const playbackActive = !!payload.playbackActive;
    const playbackPaused = !!payload.playbackPaused;
    const playbackCompletedState = !!payload.playbackCompleted && !playbackActive;
    const chaosActive = !!payload.chaosActive;
    const chaosStepsRun = Number(payload.chaosStepsRun || 0);
    const chaosMaxSteps = Number(payload.chaosMaxSteps || 0);
    const chaosTimeBudgetMs = Number(payload.chaosTimeBudgetMs || 0);
    const chaosTransitions = Number(payload.chaosTransitions || 0);
    const chaosUniqueScreens = Number(payload.chaosUniqueScreens || 0);
    const chaosCompleted = !!payload.chaosCompleted || (!chaosActive && chaosStepsRun > 0);
    const chaosHasData = chaosActive || chaosStepsRun > 0 || !!payload.chaosLoadedRunID;
    const playbackStep = Number(payload.playbackStep || 0);
    const playbackTotal = Number(payload.playbackStepTotal || 0);

    let mode = '';
    let chip = '';
    let metadata = '';
    const chaosStepsRemaining =
      chaosMaxSteps > 0 ? Math.max(0, chaosMaxSteps - chaosStepsRun) : 0;
    const chaosTimeRemaining = (() => {
      if (!chaosActive || chaosTimeBudgetMs <= 0 || !payload.chaosStartedAt) {
        return '';
      }
      const started = new Date(payload.chaosStartedAt);
      if (Number.isNaN(started.getTime())) {
        return '';
      }
      const remainingMs = chaosTimeBudgetMs - (Date.now() - started.getTime());
      return formatDurationMsCompact(Math.max(0, remainingMs));
    })();
    const chaosRemainingLabel = joinParts([
      chaosTimeRemaining ? `${chaosTimeRemaining} left` : '',
      chaosMaxSteps > 0 ? `${chaosStepsRemaining} steps left` : '',
    ]);

    // Priority order: active recording, active playback, active chaos, then completed/ready states.
    if (recordingActive) {
      mode = 'recording';
      chip = 'REC';
      metadata = joinParts([
        'capturing',
        `${Number(payload.recordingSteps || 0)} steps`,
        formatElapsed(payload.recordingStartedAt),
      ]);
    } else if (playbackActive) {
      const modeLabel = (payload.playbackMode || '').toLowerCase() === 'debug' ? 'debug' : 'play';
      const progress =
        playbackStep > 0
          ? `step ${playbackStep}${playbackTotal > 0 ? `/${playbackTotal}` : ''}`
          : 'initializing';
      mode = 'playback';
      chip = modeLabel === 'debug' ? 'DBG' : 'PLAY';
      metadata = joinParts([
        `${modeLabel} ${playbackPaused ? 'paused' : 'running'}`,
        progress,
        payload.playbackStepType || '',
        formatElapsed(payload.playbackStartedAt),
      ]);
    } else if (chaosActive) {
      mode = 'chaos';
      chip = 'CHAOS';
      metadata = joinParts([
        'running',
        `${chaosStepsRun} attempts`,
        chaosUniqueScreens > 0 ? `${chaosUniqueScreens} screens` : '',
        chaosTransitions > 0 ? `${chaosTransitions} transitions` : '',
        chaosRemainingLabel,
        formatElapsed(payload.chaosStartedAt),
      ]);
    } else if (playbackCompletedState || playbackStep > 0) {
      mode = 'playback';
      chip = 'PLAY';
      metadata = joinParts([
        playbackCompletedState ? 'complete' : 'idle',
        playbackStep > 0
          ? `step ${playbackStep}${playbackTotal > 0 ? `/${playbackTotal}` : ''}`
          : '',
        payload.playbackStepType || '',
      ]);
    } else if (chaosHasData) {
      const stoppedAt = formatStoppedAt(payload.chaosStoppedAt);
      mode = 'chaos';
      chip = 'CHAOS';
      metadata = joinParts([
        chaosCompleted ? 'complete' : 'loaded',
        chaosStepsRun > 0 ? `${chaosStepsRun} attempts` : '',
        chaosUniqueScreens > 0 ? `${chaosUniqueScreens} screens` : '',
        chaosTransitions > 0 ? `${chaosTransitions} transitions` : '',
        payload.chaosLoadedRunID ? `run ${payload.chaosLoadedRunID}` : '',
        stoppedAt ? `ended ${stoppedAt}` : '',
      ]);
    } else {
      mode = '';
      chip = '';
      metadata = '';
    }

    if (!mode || !metadata) {
      dismissedActiveRunKey = '';
      if (activeRunRow) {
        delete activeRunRow.dataset.dismissKey;
      }
      setHidden(activeRunExtend, true);
      setHidden(activeRunClose, true);
      setHidden(activeRunRow, true);
      setHidden(activeRunContainer, true);
      return;
    }

    const dismissKey = activeRunDismissKeyForPayload(payload, mode);
    if (activeRunRow) {
      activeRunRow.dataset.dismissKey = dismissKey;
    }
    if (dismissedActiveRunKey && dismissKey && dismissedActiveRunKey === dismissKey) {
      setHidden(activeRunExtend, true);
      setHidden(activeRunClose, true);
      setHidden(activeRunRow, true);
      setHidden(activeRunContainer, true);
      return;
    }
    if (dismissedActiveRunKey && dismissKey && dismissedActiveRunKey !== dismissKey) {
      dismissedActiveRunKey = '';
    }

    const canExtendChaosFromRow =
      mode === 'chaos' &&
      !chaosActive &&
      !!payload.chaosLoadedRunID &&
      !!activeRunExtend &&
      !!chaosExtendButton &&
      !chaosExtendButton.hidden;
    const showCloseButton = !recordingActive && !playbackActive && !chaosActive && !!dismissKey;
    const displayText = metadata;
    setHidden(activeRunContainer, false);
    setHidden(activeRunRow, false);
    setHidden(activeRunExtend, !canExtendChaosFromRow);
    if (activeRunExtend && chaosExtendButton) {
      activeRunExtend.disabled = !!chaosExtendButton.disabled;
    }
    setHidden(activeRunClose, !showCloseButton);
    activeRunChip.dataset.runMode = mode;
    activeRunChip.textContent = chip;
    activeRunMeta.textContent = displayText;
    activeRunMeta.removeAttribute('title');
    activeRunRow.setAttribute('aria-label', `${chip}: ${metadata}`);
    activeRunRow.removeAttribute('title');
    activeRunRow.setAttribute('data-tippy-content', `${chip}: ${metadata}`);
    if (activeRunRow._tippy) {
      activeRunRow._tippy.setContent(`${chip}: ${metadata}`);
    }
  };

  const updateWorkflowStatus = (payload) => {
    if (!payload) {
      return;
    }
    const prevActive = lastActive;
    const prevRecording = lastRecordingActive;
    const prevPlaybackCompleted = lastPlaybackCompleted;
    const prevLoadedWorkflowStepTotal = lastLoadedWorkflowStepTotal;
    lastPayload = payload;
    lastActive = !!payload.playbackActive;
    lastChaosActive = !!payload.chaosActive;
    lastPaused = !!payload.playbackPaused;
    lastRecordingActive = !!payload.recordingActive;
    lastPlaybackCompleted = !!payload.playbackCompleted && !lastActive;
    lastLoadedWorkflowStepTotal = Number(payload.loadedWorkflowStepTotal || payload.playbackStepTotal || 0);
    body.dataset.playbackActive = lastActive ? 'true' : 'false';
    body.dataset.playbackPaused = lastPaused ? 'true' : 'false';
    body.dataset.playbackCompleted = payload.playbackCompleted ? 'true' : 'false';
    body.dataset.recordingActive = lastRecordingActive ? 'true' : 'false';
    body.dataset.chaosActive = lastChaosActive ? 'true' : 'false';

    // Fire transition notifications
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.notify) {
      const notify = window.ThreeSeventyWeb.notify;
      if (!prevRecording && lastRecordingActive) {
        notify('Recording started.', 'info');
      } else if (prevRecording && !lastRecordingActive) {
        notify('Recording stopped.', 'success');
      }
      if (!prevActive && lastActive) {
        const mode = (payload.playbackMode || '').toLowerCase() === 'debug' ? 'Debug' : 'Playback';
        notify(mode + ' started.', 'info');
      } else if (prevActive && !lastActive) {
        if (!prevPlaybackCompleted && lastPlaybackCompleted) {
          notify('Playback completed.', 'success');
        } else {
          notify('Playback stopped.', 'info');
        }
      } else if (!prevPlaybackCompleted && lastPlaybackCompleted) {
        notify('Playback completed.', 'success');
      }
      if (
        lastLoadedWorkflowStepTotal > 0 &&
        lastLoadedWorkflowStepTotal !== prevLoadedWorkflowStepTotal &&
        !lastActive &&
        !lastRecordingActive
      ) {
        notify(
          `Recording loaded: Steps ${lastLoadedWorkflowStepTotal}.`,
          'success'
        );
      }
    }

    updatePlaybackControls(payload);
    updateLoadedWorkflowTooltips(payload);
    updateActiveRunRow(payload);
    if (!trackingEnabled) {
      return;
    }
    const hasPlaybackStep = typeof payload.playbackStep === 'number' && payload.playbackStep > 0;
    const chaosActive = !!payload.chaosActive;
    const chaosStepsRun = Number(payload.chaosStepsRun || 0);
    const chaosCompleted = !!payload.chaosCompleted || (!chaosActive && chaosStepsRun > 0);
    const chaosHasData = chaosActive || chaosStepsRun > 0;
    const playbackCompletedState = !!payload.playbackCompleted && !payload.playbackActive;
    const preferChaosStatus = chaosHasData && !payload.playbackActive && !payload.recordingActive && !playbackCompletedState;
    const chaosLastAttempt = payload.chaosLastAttempt || null;

    let stepLabel = payload.playbackStepLabel || '';
    let typeText = payload.playbackStepType ? `Type: ${payload.playbackStepType}` : '';
    let rangeText = payload.playbackDelayRange ? `Delay range: ${payload.playbackDelayRange}` : '';
    let appliedText = payload.playbackDelayApplied ? `Applied delay: ${payload.playbackDelayApplied}` : '';
    let eventsHtml = renderEvents(payload.playbackEvents);

    if (!stepLabel && hasPlaybackStep) {
      stepLabel = `Step ${payload.playbackStep}`;
      if (payload.playbackStepTotal && payload.playbackStepTotal > 0) {
        stepLabel = `${stepLabel}/${payload.playbackStepTotal}`;
      }
      if (payload.playbackStepType) {
        stepLabel = `${stepLabel}: ${payload.playbackStepType}`;
      }
    }

    if (preferChaosStatus) {
      stepLabel =
        payload.chaosStepLabel ||
        (chaosCompleted
          ? `Chaos completed after ${chaosStepsRun} attempts`
          : `Chaos attempt ${chaosStepsRun}`);
      if (chaosCompleted) {
        const stoppedAt = formatStoppedAt(payload.chaosStoppedAt);
        typeText = stoppedAt ? `Status: Complete at ${stoppedAt}` : 'Status: Complete';
      } else if (chaosLastAttempt && chaosLastAttempt.aidKey) {
        typeText = `AID: ${chaosLastAttempt.aidKey}`;
      } else {
        typeText = '';
      }
      if (chaosLastAttempt) {
        rangeText = `Writes: ${chaosLastAttempt.fieldsWritten || 0}/${chaosLastAttempt.fieldsTargeted || 0}`;
        const fromHash = chaosLastAttempt.fromHash || '';
        const toHash = chaosLastAttempt.toHash || '';
        if (chaosLastAttempt.error) {
          appliedText = `Error: ${chaosLastAttempt.error}`;
        } else if (fromHash || toHash) {
          appliedText = `Screen: ${fromHash || 'n/a'} -> ${toHash || 'n/a'}`;
        } else {
          appliedText = `Transitioned: ${chaosLastAttempt.transitioned ? 'yes' : 'no'}`;
        }
      } else {
        rangeText = '';
        appliedText = payload.chaosError ? `Error: ${payload.chaosError}` : '';
      }
      eventsHtml = renderEvents(payload.chaosEvents);
    } else if (!hasPlaybackStep) {
      stepLabel = placeholderText;
      typeText = '';
      rangeText = '';
      appliedText = '';
      eventsHtml = renderEvents(payload.playbackEvents);
    }

    const applyLines = (target) => {
      if (!target) {
        return;
      }
      if (target.step) {
        target.step.textContent = stepLabel;
      }
      if (target.type) {
        target.type.textContent = typeText;
        target.type.hidden = !typeText;
      }
      if (target.delayRange) {
        target.delayRange.textContent = rangeText;
        target.delayRange.hidden = !rangeText;
      }
      if (target.delayApplied) {
        target.delayApplied.textContent = appliedText;
        target.delayApplied.hidden = !appliedText;
      }
      if (target.events) {
        target.events.innerHTML = eventsHtml;
      }
    };

    applyLines(widgetLines);
    const shouldAutoScroll =
      (payload.playbackActive && !payload.playbackPaused && !payload.playbackCompleted) || payload.chaosActive;
    const eventSource = preferChaosStatus ? payload.chaosEvents : payload.playbackEvents;
    if (shouldAutoScroll && widgetLines && widgetLines.events && Array.isArray(eventSource) && eventSource.length > 0) {
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

  const refreshWorkflowStatus = () => {
    return fetchWorkflowStatus().then((payload) => {
      if (payload) {
        updateWorkflowStatus(payload);
      }
      return payload;
    });
  };

  window.refreshWorkflowStatus = refreshWorkflowStatus;

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

  if (activeRunClose) {
    activeRunClose.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      dismissedActiveRunKey = (activeRunRow && activeRunRow.dataset.dismissKey) || 'manual';
      hideActiveRunRow();
    });
    activeRunClose.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.stopPropagation();
      }
    });
  }

  if (activeRunExtend) {
    activeRunExtend.addEventListener('click', (event) => {
      event.preventDefault();
      event.stopPropagation();
      if (!chaosExtendButton || chaosExtendButton.hidden || chaosExtendButton.disabled) {
        return;
      }
      chaosExtendButton.click();
    });
    activeRunExtend.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.stopPropagation();
      }
    });
  }

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
  let lastScreenHtml = null;
  const initialScreenContainer = document.querySelector('.screen-container');
  if (initialScreenContainer) {
    lastScreenHtml = initialScreenContainer.innerHTML;
  }

  const shouldSkipScreenUpdate = (container, force) => {
    if (force) {
      return false;
    }
    if (!container) {
      return true;
    }
    const active = document.activeElement;
    return active && container.contains(active);
  };

  const updateScreenContent = (container, options = {}) => {
    const force = !!options.force;
    if (!container || shouldSkipScreenUpdate(container, force)) {
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
        if (payload.html === lastScreenHtml) {
          return;
        }
        lastScreenHtml = payload.html;
        container.innerHTML = payload.html;
        if (typeof window.installKeyHandler === 'function') {
          const form = container.querySelector('form.renderer-form');
          const formId = form ? (form.id || form.getAttribute('name')) : null;
          window.installKeyHandler(formId);
        }
        if (typeof window.sizeScreenContainer === 'function') {
          window.sizeScreenContainer();
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
    refreshWorkflowStatus()
      .then((payload) => {
        const isActive = payload && payload.playbackActive;
        const isPaused = payload && payload.playbackPaused;
        const isDebugMode = payload && String(payload.playbackMode || '').toLowerCase() === 'debug';
        const chaosActive = payload && payload.chaosActive;
        const container = document.querySelector('.screen-container');
        if ((isActive && (!isPaused || isDebugMode)) || chaosActive) {
          return updateScreenContent(container, { force: true }).then(() => true);
        }
        return false;
      })
      .finally(() => {
        const delay = (lastActive || lastChaosActive) ? playbackFastMs : playbackSlowMs;
        playbackPollTimer = window.setTimeout(pollPlayback, delay);
      });
  };

  if (playbackPollTimer === null) {
    playbackPollTimer = window.setTimeout(pollPlayback, playbackFastMs);
  }

  const removeRecordingForm = document.querySelector('form[action="/workflow/remove"]');
  if (removeRecordingForm) {
    removeRecordingForm.addEventListener('submit', () => {
      hideActiveRunRow();
    });
  }

  const playbackStartForms = document.querySelectorAll('form[action="/workflow/play"], form[action="/workflow/debug"]');
  playbackStartForms.forEach((form) => {
    form.addEventListener('submit', () => {
      hideActiveRunRow();
    });
  });

  const restoreSubmitterState = (submitter) => {
    if (submitter && typeof submitter._restoreState === 'function') {
      submitter._restoreState();
    }
  };

  const postFormAsync = (form) => {
    const method = (form.getAttribute('method') || 'post').toUpperCase();
    const body = new URLSearchParams(new FormData(form));
    return fetch(form.action, {
      method,
      headers: {
        Accept: 'text/html,application/xhtml+xml',
        'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
        'Cache-Control': 'no-cache',
      },
      credentials: 'same-origin',
      body: body.toString(),
    });
  };

  const wait = (ms) =>
    new Promise((resolve) => {
      window.setTimeout(resolve, ms);
    });

  const workflowAsyncActions = new Set([
    '/workflow/play',
    '/workflow/debug',
    '/workflow/pause',
    '/workflow/step',
    '/workflow/stop',
    '/workflow/remove',
  ]);

  document.addEventListener('submit', (event) => {
    if (event.defaultPrevented) {
      return;
    }
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) {
      return;
    }
    const actionUrl = new URL(form.action, window.location.origin);
    if (!workflowAsyncActions.has(actionUrl.pathname)) {
      return;
    }

    event.preventDefault();

    const submitter = event.submitter;
    const container = document.querySelector('.screen-container');
    const actionPath = actionUrl.pathname;
    if (
      actionPath === '/workflow/play' ||
      actionPath === '/workflow/debug' ||
      actionPath === '/workflow/remove'
    ) {
      hideActiveRunRow();
    }

    postFormAsync(form)
      .catch(() => null)
      .then(() => {
        // Step/debug start can complete just after the POST returns.
        if (actionPath === '/workflow/step' || actionPath === '/workflow/debug') {
          return wait(120);
        }
        return null;
      })
      .then(() => refreshWorkflowStatus())
      .then((payload) => {
        const shouldRefreshScreen =
          !!payload &&
          (!!payload.chaosActive ||
            !!payload.playbackActive ||
            actionPath === '/workflow/step' ||
            actionPath === '/workflow/stop' ||
            actionPath === '/workflow/remove');
        if (!shouldRefreshScreen) {
          return null;
        }
        return updateScreenContent(container, { force: true });
      })
      .finally(() => {
        restoreSubmitterState(submitter);
      });
  });
})();
