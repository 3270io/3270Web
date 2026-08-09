(() => {
  const trigger = document.querySelector('[data-disconnect-open]');
  const modal = document.querySelector('[data-disconnect-modal]');
  if (!trigger || !modal) {
    return;
  }

  const closeButtons = modal.querySelectorAll('[data-disconnect-close]');
  const confirmButton = modal.querySelector('[data-disconnect-confirm]');
  const description = modal.querySelector('#disconnect-modal-desc');

  // Which session this ends, by name.
  //
  // Disconnect has only ever ended the one you are looking at, and the others
  // keep running — but with a row of tabs above it, "Are you sure you want to
  // disconnect?" does not say which one it means, and the answer matters most
  // to somebody with three open. Written when the dialog opens rather than
  // rendered with the page, because switching tabs does not reload it.
  const describe = () => {
    if (!description) {
      return;
    }
    const tabs = window.ThreeSeventyWeb &&
      window.ThreeSeventyWeb.sessionTabs &&
      typeof window.ThreeSeventyWeb.sessionTabs.list === 'function'
      ? window.ThreeSeventyWeb.sessionTabs.list()
      : [];
    const current = tabs.filter((tab) => tab.active)[0];
    if (!current) {
      description.textContent = 'Are you sure you want to disconnect?';
      return;
    }
    const name = current.label || current.target || 'this session';
    const others = tabs.length - 1;
    if (others === 1) {
      description.textContent = 'Disconnect from ' + name + '? Your other session stays open.';
    } else if (others > 1) {
      description.textContent = 'Disconnect from ' + name + '? Your other ' + others + ' sessions stay open.';
    } else {
      description.textContent = 'Disconnect from ' + name + '?';
    }
  };

  // Focus, the Tab trap, the background scroll lock and the focus restore all
  // belong to pushModal/popModal — see modal-utils.js. Ending the session is
  // the one thing here worth an explicit initial focus: the confirm button,
  // not the cancel button that happens to come first in the markup.
  const openModal = (event) => {
    if (event) {
      event.preventDefault();
    }
    describe();
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
