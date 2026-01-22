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
  const isStatusModalOpen = () => {
    const modal = document.querySelector('[data-status-modal]');
    if (modal && !modal.hidden) {
      return true;
    }
    try {
      return localStorage.getItem('workflowStatusModalOpen') === '1';
    } catch (err) {
      return false;
    }
  };
  const tick = () => {
    if (document.visibilityState !== 'visible' || isStatusModalOpen()) {
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

  const openModal = () => {
    modal.removeAttribute('hidden');
    saveModalState(true);
    keepInBounds();
  };

  const closeModal = () => {
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
    try {
      const rect = modal.getBoundingClientRect();
      const pos = { left: rect.left, top: rect.top };
      localStorage.setItem('workflowStatusModalPos', JSON.stringify(pos));
    } catch (err) {
      // ignore
    }
  };

  const saveSize = () => {
    try {
      const size = { width: modal.offsetWidth, height: modal.offsetHeight };
      localStorage.setItem('workflowStatusModalSize', JSON.stringify(size));
    } catch (err) {
      // ignore
    }
  };

  const restoreModal = () => {
    try {
      const pos = JSON.parse(localStorage.getItem('workflowStatusModalPos') || 'null');
      if (pos && typeof pos.left === 'number' && typeof pos.top === 'number') {
        modal.style.left = `${pos.left}px`;
        modal.style.top = `${pos.top}px`;
      }
      const size = JSON.parse(localStorage.getItem('workflowStatusModalSize') || 'null');
      if (size && typeof size.width === 'number' && typeof size.height === 'number') {
        modal.style.width = `${size.width}px`;
        modal.style.height = `${size.height}px`;
      }
      const open = localStorage.getItem('workflowStatusModalOpen') === '1';
      if (open) {
        modal.removeAttribute('hidden');
        keepInBounds();
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
