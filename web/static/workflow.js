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
          trigger.innerHTML =
            '<span class="spinner" aria-hidden="true"></span> Loading...';
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
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
  };

  const closeModal = () => {
    modal.hidden = true;
    document.body.style.overflow = '';
  };

  openButtons.forEach((button) => {
    button.addEventListener('click', openModal);
  });

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
  const openTriggers = document.querySelectorAll('[data-status-open]');
  const modal = document.querySelector('[data-status-modal]');
  const closeBtn = document.querySelector('[data-status-close]');
  const dragHandle = document.querySelector('[data-status-drag]');
  const statusPanel = document.querySelector('[data-status-panel]');
  const statusPanelToggle = document.querySelector('[data-status-panel-toggle]');
  const statusPanelMeta = document.querySelector('[data-status-panel-meta]');
  const statusPanelBody = document.querySelector('[data-status-panel-body]');
  const modalOpenButtons = document.querySelectorAll('[data-status-modal-open]');
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

  const panelLines = statusPanel
    ? {
        step: statusPanel.querySelector('[data-status-step-line]'),
        type: statusPanel.querySelector('[data-status-type-line]'),
        delayRange: statusPanel.querySelector('[data-status-delay-range-line]'),
        delayApplied: statusPanel.querySelector('[data-status-delay-applied-line]'),
        events: statusPanel.querySelector('[data-status-events]'),
      }
    : null;
  const modalLines = modal
    ? {
        step: modal.querySelector('[data-status-step-line]'),
        type: modal.querySelector('[data-status-type-line]'),
        delayRange: modal.querySelector('[data-status-delay-range-line]'),
        delayApplied: modal.querySelector('[data-status-delay-applied-line]'),
        events: modal.querySelector('[data-status-events]'),
      }
    : null;
  const statusIndicators = Array.from(document.querySelectorAll('[data-status-indicator]'));

  const keepInBounds = () => {
    if (!modal) {
      return;
    }
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

  const placeholderText = 'Playback has not started yet.';
  let lastActive = body.dataset.playbackActive === 'true';
  let lastPaused = body.dataset.playbackPaused === 'true';
  let lastPayload = null;
  let useModal = false;

  const setHidden = (el, hidden) => {
    if (el) {
      el.hidden = !!hidden;
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
      playbackPauseButton.textContent = paused ? 'Resume' : 'Pause';
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
    const appliedText = payload.playbackDelayApplied ? `Applied: ${payload.playbackDelayApplied}` : '';
    if (statusPanelMeta) {
      statusPanelMeta.textContent = payload.playbackActive ? (payload.playbackPaused ? 'Paused' : 'Live') : 'Idle';
    }

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

    if (useModal) {
      applyLines(modalLines);
      if (modalLines && modalLines.events && payload.playbackEvents && payload.playbackEvents.length > 0) {
        modalLines.events.scrollTo({ top: modalLines.events.scrollHeight, behavior: 'smooth' });
      }
      return;
    }

    applyLines(panelLines);
    if (statusPanelBody && payload.playbackEvents && payload.playbackEvents.length > 0) {
      if (statusPanel && statusPanel.classList.contains('is-collapsed')) {
        return;
      }
      const maxScroll = statusPanelBody.scrollHeight - statusPanelBody.clientHeight;
      if (maxScroll > 0) {
        statusPanelBody.scrollTo({ top: statusPanelBody.scrollHeight, behavior: 'smooth' });
      }
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

  const openModal = () => {
    if (!modal) {
      return;
    }
    modal.removeAttribute('hidden');
    saveModalState(true);
    keepInBounds();
    useModal = true;
    if (lastPayload) {
      updateWorkflowStatus(lastPayload);
    }
  };

  const closeModal = () => {
    if (!modal) {
      return;
    }
    modal.setAttribute('hidden', '');
    saveModalState(false);
    useModal = false;
    if (lastPayload) {
      updateWorkflowStatus(lastPayload);
    }
  };

  openTriggers.forEach((trigger) => {
    trigger.addEventListener('click', openModal);
    trigger.addEventListener('keydown', (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        openModal();
      }
    });
  });

  modalOpenButtons.forEach((button) => {
    button.addEventListener('click', (event) => {
      event.stopPropagation();
      openModal();
    });
  });

  document.addEventListener('click', (event) => {
    const trigger = event.target.closest('[data-status-open]');
    if (trigger) {
      openModal();
    }
  });

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return;
    }
    const active = document.activeElement;
    if (active && active.closest && active.closest('[data-status-open]')) {
      event.preventDefault();
      openModal();
    }
  });
  if (closeBtn) {
    closeBtn.addEventListener('click', closeModal);
  }

  if (dragHandle) {
    let dragging = false;
    let startX = 0;
    let startY = 0;
    let startLeft = 0;
    let startTop = 0;

    const onMove = (event) => {
      if (!dragging) {
        return;
      }
      const dx = event.clientX - startX;
      const dy = event.clientY - startY;
      modal.style.left = `${startLeft + dx}px`;
      modal.style.top = `${startTop + dy}px`;
      savePosition();
    };

    const onUp = () => {
      dragging = false;
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    };

    dragHandle.addEventListener('mousedown', (event) => {
      dragging = true;
      const rect = modal.getBoundingClientRect();
      startX = event.clientX;
      startY = event.clientY;
      startLeft = rect.left;
      startTop = rect.top;
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    });
  }

  const saveModalState = (isOpen) => {
    try {
      localStorage.setItem('workflowStatusModalOpen', isOpen ? '1' : '0');
      savePosition();
      saveSize();
    } catch (err) {
      // ignore
    }
  };

  const savePosition = () => {
    if (!modal || modal.hidden) {
      return;
    }
    try {
      const rect = modal.getBoundingClientRect();
      const pos = { left: rect.left, top: rect.top };
      localStorage.setItem('workflowStatusModalPos', JSON.stringify(pos));
    } catch (err) {
      // ignore
    }
  };

  const saveSize = () => {
    if (!modal || modal.hidden) {
      return;
    }
    try {
      const size = { width: modal.offsetWidth, height: modal.offsetHeight };
      localStorage.setItem('workflowStatusModalSize', JSON.stringify(size));
    } catch (err) {
      // ignore
    }
  };

  const restoreModal = () => {
    if (!modal) {
      return;
    }
    try {
      const pos = JSON.parse(localStorage.getItem('workflowStatusModalPos') || 'null');
      if (pos && typeof pos.left === 'number' && typeof pos.top === 'number') {
        modal.style.left = `${pos.left}px`;
        modal.style.top = `${pos.top}px`;
      }
      const size = JSON.parse(localStorage.getItem('workflowStatusModalSize') || 'null');
      if (size && typeof size.width === 'number' && size.width >= 40) {
        modal.style.width = `${size.width}px`;
      }
      if (size && typeof size.height === 'number' && size.height >= 40) {
        modal.style.height = `${size.height}px`;
      }
      const open = localStorage.getItem('workflowStatusModalOpen') === '1';
      if (open) {
        modal.removeAttribute('hidden');
        keepInBounds();
        useModal = true;
        if (lastPayload) {
          updateWorkflowStatus(lastPayload);
        }
      }
    } catch (err) {
      // ignore
    }
  };

  if (typeof ResizeObserver !== 'undefined') {
    if (modal) {
      const observer = new ResizeObserver(() => {
        saveSize();
      });
      observer.observe(modal);
    }
  }

  restoreModal();

  const panelKey = 'workflowStatusPanelCollapsed';
  const setPanelCollapsed = (collapsed) => {
    if (!statusPanel) {
      return;
    }
    statusPanel.classList.toggle('is-collapsed', collapsed);
    if (statusPanelToggle) {
      statusPanelToggle.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
    }
    try {
      localStorage.setItem(panelKey, collapsed ? '1' : '0');
    } catch (err) {
      // ignore
    }
  };

  if (statusPanelToggle) {
    statusPanelToggle.addEventListener('click', () => {
      const collapsed = statusPanel && statusPanel.classList.contains('is-collapsed');
      setPanelCollapsed(!collapsed);
    });
  }

  try {
    const collapsed = localStorage.getItem(panelKey) === '1';
    setPanelCollapsed(collapsed);
  } catch (err) {
    // ignore
  }

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
