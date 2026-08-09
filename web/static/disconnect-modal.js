(() => {
  const trigger = document.querySelector('[data-disconnect-open]');
  const modal = document.querySelector('[data-disconnect-modal]');
  if (!trigger || !modal) {
    return;
  }

  const closeButtons = modal.querySelectorAll('[data-disconnect-close]');
  const confirmButton = modal.querySelector('[data-disconnect-confirm]');

  // Focus, the Tab trap, the background scroll lock and the focus restore all
  // belong to pushModal/popModal — see modal-utils.js. Ending the session is
  // the one thing here worth an explicit initial focus: the confirm button,
  // not the cancel button that happens to come first in the markup.
  const openModal = (event) => {
    if (event) {
      event.preventDefault();
    }
    modal.hidden = false;
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
      window.ThreeSeventyWeb.pushModal(modal, closeModal, { initialFocus: confirmButton });
    } else if (confirmButton) {
      confirmButton.focus();
    }
  };

  const closeModal = () => {
    modal.hidden = true;
    if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
      window.ThreeSeventyWeb.popModal(modal);
    } else {
      trigger.focus();
    }
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
