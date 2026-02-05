(() => {
  const modal = document.querySelector('[data-logs-modal]');
  if (!modal) {
    return;
  }

  const openButtons = document.querySelectorAll('[data-logs-open]');
  const closeButtons = modal.querySelectorAll('[data-logs-modal-close]');
  const logsContent = modal.querySelector('[data-logs-content]');
  const logsToggle = modal.querySelector('[data-logs-toggle-checkbox]');
  const refreshButton = modal.querySelector('[data-logs-refresh]');
  const clearButton = modal.querySelector('[data-logs-clear]');
  const copyButton = modal.querySelector('[data-logs-copy]');
  const downloadButton = modal.querySelector('[data-logs-download]');
  const maximizeButton = modal.querySelector('[data-logs-maximize]');
  const minimizeButton = modal.querySelector('[data-logs-minimize]');

  const storageKeyEnabled = '3270Web.verboseLogging';
  const storageKeyMaximized = '3270Web.logsModalMaximized';

  let autoRefreshInterval = null;
  let isMaximized = false;
  let lastFocusedElement = null;

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
    fetchLogs();
    startAutoRefresh();
  };

  const closeModal = () => {
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
      if (!modal.hidden && logsToggle && logsToggle.checked) {
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
          if (logsContent) {
            logsContent.textContent =
              'Log access is disabled by the administrator.';
            setLogsTooltip('Set ALLOW_LOG_ACCESS=true to enable log access.');
          }
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
    if (!confirm('Are you sure you want to clear all logs?')) {
      return;
    }
    fetch('/logs/clear', {
      method: 'POST',
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data && data.success) {
          fetchLogs();
        }
      })
      .catch((err) => {
        console.error('Failed to clear logs:', err);
      });
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
      closeModal();
    }
  });

  if (logsToggle) {
    logsToggle.addEventListener('change', () => {
      toggleVerboseLogging(logsToggle.checked);
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
