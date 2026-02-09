(() => {
  document.addEventListener("DOMContentLoaded", () => {
    const modal = document.querySelector("[data-about-modal]");
    if (!modal) {
      return;
    }

    const openButtons = document.querySelectorAll("[data-about-open]");
    const closeButtons = modal.querySelectorAll("[data-about-close]");
    let lastFocused = null;

    const closeModal = () => {
      modal.hidden = true;
      if (lastFocused && typeof lastFocused.focus === "function") {
        lastFocused.focus();
      }
      lastFocused = null;
    };

    const openModal = () => {
      lastFocused = document.activeElement;
      modal.hidden = false;
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

    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape" && !modal.hidden) {
        closeModal();
      }
    });
  });
})();
