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
  const isPlaying = body.dataset.playbackActive === 'true';
  const isPaused = body.dataset.playbackPaused === 'true';
  if (!isPlaying || isPaused) {
    return;
  }

  const refreshMs = 300;
  const tick = () => {
    if (document.visibilityState !== 'visible') {
      setTimeout(tick, refreshMs);
      return;
    }
    window.location.reload();
  };

  setTimeout(tick, refreshMs);
})();

(() => {
  const openTriggers = document.querySelectorAll('[data-status-open]');
  const modal = document.querySelector('[data-status-modal]');
  const closeBtn = document.querySelector('[data-status-close]');
  const dragHandle = document.querySelector('[data-status-drag]');

  if (!modal) {
    return;
  }

  const statusStepLine = modal.querySelector('[data-status-step-line]');
  const statusTypeLine = modal.querySelector('[data-status-type-line]');
  const statusDelayRangeLine = modal.querySelector('[data-status-delay-range-line]');
  const statusDelayAppliedLine = modal.querySelector('[data-status-delay-applied-line]');
  const statusEvents = modal.querySelector('[data-status-events]');
  const statusIndicators = Array.from(document.querySelectorAll('[data-status-indicator]'));
  const rowStepText = document.querySelector('[data-status-row-step]');
  const rowTypeText = document.querySelector('[data-status-row-type]');
  const rowDelayRangeText = document.querySelector('[data-status-row-delay-range]');
  const rowDelayAppliedText = document.querySelector('[data-status-row-delay-applied]');

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

  const statusPollIntervalMs = 1500;
  let statusPollTimer = null;
  const placeholderText = 'Playback has not started yet.';

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
    if (statusStepLine) {
      statusStepLine.textContent = stepLabel;
    }
    statusIndicators.forEach((indicator) => {
      indicator.textContent = stepLabel;
    });
    if (rowStepText) {
      rowStepText.textContent = stepLabel;
    }
    const typeText = payload.playbackStepType ? `Type: ${payload.playbackStepType}` : '';
    if (statusTypeLine) {
      statusTypeLine.textContent = typeText;
      statusTypeLine.hidden = !payload.playbackStepType;
    }
    if (rowTypeText) {
      rowTypeText.textContent = typeText;
      rowTypeText.hidden = !payload.playbackStepType;
    }
    const rangeText = payload.playbackDelayRange ? `Delay range: ${payload.playbackDelayRange}` : '';
    if (statusDelayRangeLine) {
      statusDelayRangeLine.textContent = rangeText;
      statusDelayRangeLine.hidden = !payload.playbackDelayRange;
    }
    if (rowDelayRangeText) {
      rowDelayRangeText.textContent = rangeText;
      rowDelayRangeText.hidden = !payload.playbackDelayRange;
    }
    const appliedText = payload.playbackDelayApplied ? `Applied: ${payload.playbackDelayApplied}` : '';
    if (statusDelayAppliedLine) {
      statusDelayAppliedLine.textContent = appliedText;
      statusDelayAppliedLine.hidden = !payload.playbackDelayApplied;
    }
    if (rowDelayAppliedText) {
      rowDelayAppliedText.textContent = appliedText;
      rowDelayAppliedText.hidden = !payload.playbackDelayApplied;
    }
    if (statusEvents) {
      statusEvents.innerHTML = renderEvents(payload.playbackEvents);
    }
  };

  const stopStatusPolling = () => {
    if (statusPollTimer === null) {
      return;
    }
    window.clearInterval(statusPollTimer);
    statusPollTimer = null;
  };

  const fetchWorkflowStatus = () => {
    fetch('/workflow/status', {
      headers: {
        Accept: 'application/json',
        'Cache-Control': 'no-cache',
      },
    })
      .then((res) => {
        if (res.status === 401 || res.status === 403) {
          stopStatusPolling();
          return null;
        }
        if (!res.ok) {
          return null;
        }
        return res.json();
      })
      .then((payload) => {
        if (payload) {
          updateWorkflowStatus(payload);
        }
      })
      .catch(() => {
        // ignore transient errors
      });
  };

  const startStatusPolling = () => {
    if (statusPollTimer !== null) {
      return;
    }
    fetchWorkflowStatus();
    statusPollTimer = window.setInterval(fetchWorkflowStatus, statusPollIntervalMs);
  };

  const openModal = () => {
    modal.removeAttribute('hidden');
    saveModalState(true);
    keepInBounds();
    startStatusPolling();
  };

  const closeModal = () => {
    stopStatusPolling();
    modal.setAttribute('hidden', '');
    saveModalState(false);
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
    if (modal.hidden) {
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
    if (modal.hidden) {
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
    stopStatusPolling();
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
        startStatusPolling();
      }
    } catch (err) {
      // ignore
    }
  };

  if (typeof ResizeObserver !== 'undefined') {
    const observer = new ResizeObserver(() => {
      saveSize();
    });
    observer.observe(modal);
  }

  restoreModal();
})();
