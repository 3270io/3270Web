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
  const statusRow = document.querySelector('[data-status-open]');
  const modal = document.querySelector('[data-status-modal]');
  const closeBtn = document.querySelector('[data-status-close]');
  const dragHandle = document.querySelector('[data-status-drag]');

  if (!statusRow || !modal) {
    return;
  }

  const openModal = () => {
    modal.hidden = false;
    saveModalState(true);
  };

  const closeModal = () => {
    modal.hidden = true;
    saveModalState(false);
  };

  statusRow.addEventListener('click', openModal);
  statusRow.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' || event.key === ' ') {
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
        modal.hidden = false;
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
