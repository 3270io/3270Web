(() => {
  document.addEventListener("DOMContentLoaded", () => {
    const modal = document.querySelector("[data-about-modal]");
    if (!modal) {
      return;
    }

    const openButtons = document.querySelectorAll("[data-about-open]");
    const closeButtons = modal.querySelectorAll("[data-about-close]");
    let lastFocused = null;
    const focusTrap =
      window.ThreeSeventyWeb && window.ThreeSeventyWeb.createFocusTrap
        ? window.ThreeSeventyWeb.createFocusTrap(modal)
        : { activate() {}, deactivate() {} };

    const closeModal = () => {
      modal.hidden = true;
      document.body.style.overflow = "";
      focusTrap.deactivate();
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.popModal) {
        window.ThreeSeventyWeb.popModal(modal);
      }
      if (lastFocused && typeof lastFocused.focus === "function") {
        lastFocused.focus();
      }
      lastFocused = null;
    };

    const openModal = () => {
      lastFocused = document.activeElement;
      modal.hidden = false;
      document.body.style.overflow = "hidden";
      focusTrap.activate();
      if (window.ThreeSeventyWeb && window.ThreeSeventyWeb.pushModal) {
        window.ThreeSeventyWeb.pushModal(modal, closeModal);
      }
      const firstFocusable = modal.querySelector("button, a, input, select, textarea, [tabindex]:not([tabindex='-1'])");
      if (firstFocusable) {
        firstFocusable.focus();
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
