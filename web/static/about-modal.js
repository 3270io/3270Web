(() => {
  document.addEventListener("DOMContentLoaded", () => {
    const modal = document.querySelector("[data-about-modal]");
    if (!modal) {
      return;
    }

    const openButtons = document.querySelectorAll("[data-about-open]");
    const closeButtons = modal.querySelectorAll("[data-about-close]");

    // Focus, the Tab trap, the background scroll lock and the focus restore
    // all belong to pushModal/popModal — see modal-utils.js.
    const closeModal = () => {
      modal.hidden = true;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
    };

    const openModal = () => {
      modal.hidden = false;
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
        window.ThreeSeventyWeb.pushModal(modal, closeModal);
      }
    };

    openButtons.forEach((button) => {
      button.addEventListener("click", openModal);
    });
    closeButtons.forEach((button) => {
      button.addEventListener("click", closeModal);
    });

    modal.addEventListener("click", (event) => {
      if (event.target === modal) {
        closeModal();
      }
    });
  });
})();
