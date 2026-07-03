(() => {
  const trigger = document.querySelector('[data-disconnect-open]');
  const modal = document.querySelector('[data-disconnect-modal]');
  if (!trigger || !modal) {
    return;
  }

  const closeButtons = modal.querySelectorAll('[data-disconnect-close]');
  const confirmButton = modal.querySelector('[data-disconnect-confirm]');
  const focusTrap =
    window.ThreeSeventyWeb && window.ThreeSeventyWeb.createFocusTrap
      ? window.ThreeSeventyWeb.createFocusTrap(modal)
      : { activate() {}, deactivate() {} };

  const openModal = (event) => {
    if (event) {
      event.preventDefault();
    }
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
    focusTrap.activate();
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      window.ThreeSeventyWeb.pushModal(modal, closeModal);
    }
    if (confirmButton) {
      confirmButton.focus();
    }
  };

  const closeModal = () => {
    modal.hidden = true;
    document.body.style.overflow = '';
    focusTrap.deactivate();
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
      window.ThreeSeventyWeb.popModal(modal);
    }
    trigger.focus();
  };

  trigger.addEventListener('click', openModal);

  closeButtons.forEach((button) => {
    button.addEventListener('click', closeModal);
  });

  modal.addEventListener('click', (event) => {
    if (event.target === modal) {
      closeModal();
    }
  });
})();
