(() => {
  const trigger = document.querySelector('[data-disconnect-open]');
  const modal = document.querySelector('[data-disconnect-modal]');
  if (!trigger || !modal) {
    return;
  }

  const closeButtons = modal.querySelectorAll('[data-disconnect-close]');
  const confirmButton = modal.querySelector('[data-disconnect-confirm]');

  const openModal = (event) => {
    if (event) {
      event.preventDefault();
    }
    modal.hidden = false;
    document.body.style.overflow = 'hidden';
    if (confirmButton) {
      confirmButton.focus();
    }
  };

  const closeModal = () => {
    modal.hidden = true;
    document.body.style.overflow = '';
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

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !modal.hidden) {
      closeModal();
    }
  });
})();
