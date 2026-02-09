(() => {
  const measureCellMetrics = (reference) => {
    if (!reference) {
      return null;
    }
    const probe = document.createElement("span");
    probe.textContent = "MMMMMMMMMM";
    probe.style.position = "absolute";
    probe.style.visibility = "hidden";
    probe.style.pointerEvents = "none";
    probe.style.whiteSpace = "pre";
    const styles = window.getComputedStyle(reference);
    probe.style.fontFamily = styles.fontFamily;
    probe.style.fontSize = styles.fontSize;
    probe.style.fontWeight = styles.fontWeight;
    probe.style.fontStyle = styles.fontStyle;
    probe.style.letterSpacing = styles.letterSpacing;
    probe.style.lineHeight = styles.lineHeight;
    document.body.appendChild(probe);
    const rect = probe.getBoundingClientRect();
    const charWidth = rect.width / 10;
    const rowHeight = rect.height;
    probe.remove();
    if (!Number.isFinite(charWidth) || charWidth <= 0 || !Number.isFinite(rowHeight) || rowHeight <= 0) {
      return null;
    }
    return { charWidth, rowHeight };
  };

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

    const metrics = measureCellMetrics(reference);
    if (!metrics) {
      return;
    }

    const widthPx = Math.ceil(cols * metrics.charWidth + 2);
    container.style.width = `${widthPx}px`;
    // Let content define height to avoid clipping the last terminal row.
    container.style.removeProperty("height");
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
