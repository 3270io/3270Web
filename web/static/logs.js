(() => {
  const modal = document.querySelector('[data-logs-modal]');
  if (!modal) {
    return;
  }

  const openButtons = document.querySelectorAll('[data-logs-open]');
  const closeButtons = modal.querySelectorAll('[data-logs-modal-close]');
  const logsContent = modal.querySelector('[data-logs-content]');
  const logsToggle = modal.querySelector('[data-logs-toggle-checkbox]');
  const accessToggle = modal.querySelector('[data-logs-access-checkbox]');
  const refreshButton = modal.querySelector('[data-logs-refresh]');
  const clearButton = modal.querySelector('[data-logs-clear]');
  const copyButton = modal.querySelector('[data-logs-copy]');
  const downloadButton = modal.querySelector('[data-logs-download]');
  const maximizeButton = modal.querySelector('[data-logs-maximize]');
  const minimizeButton = modal.querySelector('[data-logs-minimize]');
  const clearConfirmModal = modal.querySelector('[data-logs-clear-confirm]');
  const clearConfirmCloseButtons = modal.querySelectorAll('[data-logs-clear-close], [data-logs-clear-cancel]');
  const clearConfirmAcceptButton = modal.querySelector('[data-logs-clear-confirm-accept]');

  const storageKeyEnabled = '3270Web.verboseLogging';
  const storageKeyMaximized = '3270Web.logsModalMaximized';

  let autoRefreshInterval = null;
  let isMaximized = false;
  let lastFocusedElement = null;
  let clearConfirmReturnFocus = null;
  let logAccessEnabled = true;

  const logsToggleLabel = logsToggle
    ? logsToggle.closest('.logs-toggle-label')
    : null;
  const accessToggleLabel = accessToggle
    ? accessToggle.closest('.logs-toggle-label')
    : null;

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
    fetchLogAccess().finally(() => {
      if (logAccessEnabled) {
        fetchLogs();
        startAutoRefresh();
      }
    });
  };

  const closeModal = () => {
    closeClearConfirm();
    modal.hidden = true;
    document.body.style.overflow = '';
    stopAutoRefresh();
    if (lastFocusedElement) {
      lastFocusedElement.focus();
      lastFocusedElement = null;
    }
  };

  const startAutoRefresh = () => {
    if (autoRefreshInterval) {
      return;
    }
    autoRefreshInterval = window.setInterval(() => {
      if (
        !modal.hidden &&
        logAccessEnabled &&
        logsToggle &&
        logsToggle.checked
      ) {
        fetchLogs();
      }
    }, 2000);
  };

  const stopAutoRefresh = () => {
    if (autoRefreshInterval) {
      window.clearInterval(autoRefreshInterval);
      autoRefreshInterval = null;
    }
  };

  const setLogsTooltip = (message) => {
    if (!logsContent) {
      return;
    }
    if (message) {
      logsContent.setAttribute('data-tippy-content', message);
      if (window.tippy) {
        if (logsContent._tippy) {
          logsContent._tippy.setContent(message);
        } else {
          window.tippy(logsContent, {
            delay: [150, 0],
            placement: 'top',
          });
        }
      }
    } else {
      logsContent.removeAttribute('data-tippy-content');
      if (logsContent._tippy) {
        logsContent._tippy.destroy();
      }
    }
  };

  const fetchLogs = () => {
    return fetch('/logs', {
      headers: {
        Accept: 'application/json',
        'Cache-Control': 'no-cache',
      },
    })
      .then((res) => {
        if (res.status === 403) {
          setLogAccessState(false, { skipFetch: true });
          return null;
        }
        if (!res.ok) {
          if (logsContent) {
            logsContent.textContent = 'Unable to load logs right now.';
            setLogsTooltip('');
          }
          return null;
        }
        return res.json();
      })
      .then((data) => {
        if (!data) {
          return;
        }
        if (logsToggle) {
          logsToggle.checked = data.enabled || false;
        }
        if (logsContent) {
          const content = data.content || '';
          logsContent.textContent =
            content ||
            'No logs yet. Enable verbose logging to capture S3270 commands and responses.';
          setLogsTooltip('');
          // Auto-scroll to bottom
          logsContent.scrollTop = logsContent.scrollHeight;
        }
      })
      .catch((err) => {
        console.error('Failed to fetch logs:', err);
      });
  };

  const fetchLogAccess = () => {
    return fetch('/logs/access', {
      headers: {
        Accept: 'application/json',
        'Cache-Control': 'no-cache',
      },
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (!data) {
          return;
        }
        setLogAccessState(!!data.enabled, { skipFetch: true });
      })
      .catch((err) => {
        console.error('Failed to fetch log access state:', err);
      });
  };

  const updateLogAccessUI = () => {
    const disabled = !logAccessEnabled;
    if (logsToggle) {
      logsToggle.disabled = disabled;
    }
    if (logsToggleLabel) {
      logsToggleLabel.classList.toggle('is-disabled', disabled);
    }
    [refreshButton, clearButton, copyButton, downloadButton].forEach(
      (button) => {
        if (button) {
          button.disabled = disabled;
        }
      }
    );

    if (disabled) {
      if (logsContent) {
        logsContent.textContent =
          'Log access is disabled by the administrator.';
      }
      setLogsTooltip('Set ALLOW_LOG_ACCESS=true to enable log access.');
      stopAutoRefresh();
    } else {
      setLogsTooltip('');
    }
  };

  const setLogAccessState = (enabled, options = {}) => {
    logAccessEnabled = enabled;
    if (accessToggle) {
      accessToggle.checked = enabled;
    }
    if (accessToggleLabel) {
      accessToggleLabel.classList.toggle('is-disabled', false);
    }
    updateLogAccessUI();
    if (enabled && !options.skipFetch) {
      fetchLogs();
      startAutoRefresh();
    }
  };

  const toggleLogAccess = (enabled) => {
    const formData = new FormData();
    formData.append('enabled', enabled ? 'true' : 'false');
    fetch('/logs/access', {
      method: 'POST',
      body: formData,
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data) {
          setLogAccessState(!!data.enabled);
        }
      })
      .catch((err) => {
        console.error('Failed to toggle log access:', err);
      });
  };

  const toggleVerboseLogging = (enabled) => {
    const formData = new FormData();
    formData.append('enabled', enabled ? 'true' : 'false');
    fetch('/logs/toggle', {
      method: 'POST',
      body: formData,
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data) {
          if (logsToggle) {
            logsToggle.checked = data.enabled || false;
          }
          try {
            localStorage.setItem(storageKeyEnabled, data.enabled ? '1' : '0');
          } catch (err) {
            // ignore
          }
        }
      })
      .catch((err) => {
        console.error('Failed to toggle verbose logging:', err);
      });
  };

  const clearLogs = () => {
    openClearConfirm();
  };

  const clearLogsConfirmed = () => {
    closeClearConfirm();

    let originalHtml = '';
    if (clearButton) {
      originalHtml = clearButton.innerHTML;
      clearButton.disabled = true;
      clearButton.innerHTML =
        '<span class="spinner" aria-hidden="true"></span> Clearing...';
    }

    fetch('/logs/clear', {
      method: 'POST',
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data && data.success) {
          return fetchLogs();
        }
      })
      .catch((err) => {
        console.error('Failed to clear logs:', err);
      })
      .finally(() => {
        if (clearButton && originalHtml) {
          clearButton.innerHTML = originalHtml;
          clearButton.disabled = false;
        }
      });
  };

  const openClearConfirm = () => {
    if (!clearConfirmModal) {
      clearLogsConfirmed();
      return;
    }
    clearConfirmReturnFocus = document.activeElement;
    clearConfirmModal.hidden = false;
    const firstFocusable = clearConfirmModal.querySelector(
      'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
    );
    if (firstFocusable) {
      firstFocusable.focus();
    }
  };

  const closeClearConfirm = () => {
    if (!clearConfirmModal || clearConfirmModal.hidden) {
      return;
    }
    clearConfirmModal.hidden = true;
    if (
      clearConfirmReturnFocus &&
      typeof clearConfirmReturnFocus.focus === 'function'
    ) {
      clearConfirmReturnFocus.focus();
    }
    clearConfirmReturnFocus = null;
  };

  const downloadLogs = () => {
    window.location.href = '/logs/download';
  };

  const setMaximized = (maximized) => {
    isMaximized = maximized;
    modal.classList.toggle('is-maximized', maximized);
    if (maximizeButton) {
      maximizeButton.hidden = maximized;
      maximizeButton.setAttribute('aria-expanded', maximized ? 'true' : 'false');
    }
    if (minimizeButton) {
      minimizeButton.hidden = !maximized;
      minimizeButton.setAttribute('aria-expanded', maximized ? 'true' : 'false');
    }
    try {
      localStorage.setItem(storageKeyMaximized, maximized ? '1' : '0');
    } catch (err) {
      // ignore
    }
  };

  const restoreMaximizedState = () => {
    try {
      const maximized = localStorage.getItem(storageKeyMaximized) === '1';
      setMaximized(maximized);
    } catch (err) {
      // ignore
    }
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
      if (clearConfirmModal && !clearConfirmModal.hidden) {
        closeClearConfirm();
        return;
      }
      closeModal();
    }
  });

  if (logsToggle) {
    logsToggle.addEventListener('change', () => {
      toggleVerboseLogging(logsToggle.checked);
    });
  }

  if (accessToggle) {
    accessToggle.addEventListener('change', () => {
      toggleLogAccess(accessToggle.checked);
    });
  }

  if (refreshButton) {
    refreshButton.addEventListener('click', () => {
      const originalHtml = refreshButton.innerHTML;
      refreshButton.disabled = true;
      refreshButton.innerHTML =
        '<span class="spinner" aria-hidden="true"></span> Refreshing...';

      fetchLogs().finally(() => {
        refreshButton.innerHTML = originalHtml;
        refreshButton.disabled = false;
      });
    });
  }

  if (clearButton) {
    clearButton.addEventListener('click', clearLogs);
  }

  clearConfirmCloseButtons.forEach((button) => {
    button.addEventListener('click', closeClearConfirm);
  });

  if (clearConfirmAcceptButton) {
    clearConfirmAcceptButton.addEventListener('click', clearLogsConfirmed);
  }

  if (downloadButton) {
    downloadButton.addEventListener('click', downloadLogs);
  }

  if (copyButton) {
    copyButton.addEventListener('click', () => {
      if (!logsContent) {
        return;
      }
      const text = logsContent.textContent;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard
          .writeText(text)
          .then(() => {
            if (copyButton._tippy) {
              const original =
                copyButton.getAttribute('data-tippy-content') || 'Copy logs';
              copyButton._tippy.setContent('Copied!');
              copyButton._tippy.show();
              setTimeout(() => {
                copyButton._tippy.setContent(original);
              }, 2000);
            }
          })
          .catch((err) => {
            console.error('Failed to copy logs:', err);
          });
      }
    });
  }

  if (maximizeButton) {
    maximizeButton.addEventListener('click', () => {
      setMaximized(true);
    });
  }

  if (minimizeButton) {
    minimizeButton.addEventListener('click', () => {
      setMaximized(false);
    });
  }

  restoreMaximizedState();
})();
