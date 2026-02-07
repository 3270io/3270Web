(() => {
  const sizeScreenContainer = () => {
    const container = document.querySelector('.screen-container');
    if (!container) {
      return;
    }
    const form = container.querySelector('form.renderer-form');
    if (!form) {
      return;
    }
    const rows = Number.parseInt(form.dataset.rows, 10);
    const cols = Number.parseInt(form.dataset.cols, 10);
    if (!Number.isFinite(rows) || !Number.isFinite(cols) || rows <= 0 || cols <= 0) {
      return;
    }
    const reference =
      container.querySelector('pre') ||
      container.querySelector('input') ||
      container.querySelector('textarea');
    if (!reference) {
      return;
    }
    const lineHeight = Number.parseFloat(window.getComputedStyle(reference).lineHeight);
    if (!Number.isFinite(lineHeight) || lineHeight <= 0) {
      return;
    }
    container.style.width = `${cols}ch`;
    container.style.height = `${rows * lineHeight}px`;
  };

  window.sizeScreenContainer = sizeScreenContainer;

  document.addEventListener('DOMContentLoaded', sizeScreenContainer);

  const container = document.querySelector('.screen-container');
  if (container && typeof MutationObserver !== 'undefined') {
    const observer = new MutationObserver(() => {
      sizeScreenContainer();
    });
    observer.observe(container, { childList: true, subtree: true });
  }
})();
