import { chromium } from 'playwright';
import path from 'node:path';

const baseUrl = 'http://127.0.0.1:8080';
const repo = 'C:/Users/paul/git/h3270';
const workflowFile = path.join(repo, 'workflow.json');
const outDir = path.join(repo, 'docs/images');

async function ensureScreen(page) {
  await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(600);
  if (/\/screen(?:$|\?)/.test(page.url())) {
    await page.waitForSelector('[data-settings-open]', { timeout: 15000 });
    return;
  }
  const hostInput = page.locator('#hostname-input');
  if (await hostInput.count()) {
    await hostInput.fill('127.0.0.1:3270');
    await page.locator('#connect-btn').click();
  }

  await page.waitForURL(/\/screen/, { timeout: 30000 });
  await page.waitForSelector('[data-settings-open]', { timeout: 15000 });
}

async function addBadges(page, items) {
  await page.evaluate(() => {
    document.querySelectorAll('.doc-badge').forEach((el) => el.remove());
  });
  for (const item of items) {
    const box = await page.locator(item.selector).first().boundingBox();
    if (!box) continue;
    const x = box.x + (item.offsetX ?? 10);
    const y = box.y + (item.offsetY ?? 10);
    await page.evaluate(({ x, y, label }) => {
      const el = document.createElement('div');
      el.className = 'doc-badge';
      el.textContent = String(label);
      Object.assign(el.style, {
        position: 'fixed',
        left: `${Math.max(8, x)}px`,
        top: `${Math.max(8, y)}px`,
        width: '28px',
        height: '28px',
        borderRadius: '999px',
        background: '#ef4444',
        color: '#fff',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontWeight: '700',
        fontFamily: 'Arial, sans-serif',
        zIndex: '999999',
        border: '2px solid #fff',
        boxShadow: '0 2px 8px rgba(0,0,0,0.35)'
      });
      document.body.appendChild(el);
    }, { x, y, label: item.label });
  }
}

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1600, height: 1100 } });
  const page = await context.newPage();

  await ensureScreen(page);

  // Load workflow so play/debug controls are visible.
  const fileInput = page.locator('input[name="workflow"]');
  if (await fileInput.count()) {
    await fileInput.setInputFiles(workflowFile);
    await page.waitForURL(/\/screen/, { timeout: 20000 });
    await page.waitForSelector('[data-modal-open]', { timeout: 15000 });
    await page.waitForTimeout(600);
  }

  // Toolbar screenshot.
  await addBadges(page, [
    { selector: '[data-disconnect-open]', label: 1, offsetX: 2, offsetY: -8 },
    { selector: '[data-logs-open]', label: 2, offsetX: -6, offsetY: -10 },
    { selector: '[data-settings-open]', label: 3, offsetX: -6, offsetY: -10 },
    { selector: '[data-recording-start] button', label: 4, offsetX: -6, offsetY: -10 },
    { selector: '[data-modal-open]', label: 5, offsetX: -6, offsetY: -10 }
  ]);
  await page.screenshot({ path: path.join(outDir, 'toolbar-real.png'), fullPage: true });

  // Settings modal screenshot.
  await page.locator('[data-settings-open]').click();
  await page.waitForSelector('[data-settings-modal]:not([hidden])');
  await page.waitForSelector('.settings-tab');
  await addBadges(page, [
    { selector: '[data-settings-refresh]', label: 1, offsetX: -6, offsetY: -10 },
    { selector: '[data-settings-maximize]', label: 2, offsetX: -6, offsetY: -10 },
    { selector: '[data-settings-close]', label: 3, offsetX: -6, offsetY: -10 },
    { selector: '.settings-tab', label: 4, offsetX: -8, offsetY: -10 },
    { selector: '.settings-group.is-active', label: 5, offsetX: -6, offsetY: -10 },
    { selector: '.settings-group.is-active .settings-group-reset', label: 6, offsetX: -8, offsetY: -10 },
    { selector: '[data-settings-save]', label: 7, offsetX: -8, offsetY: -10 }
  ]);
  await page.screenshot({ path: path.join(outDir, 'settings-modal-real.png'), fullPage: true });
  await page.locator('.settings-modal-actions [data-settings-close]').click();
  await page.waitForTimeout(400);

  // Workflow controls screenshot.
  await addBadges(page, [
    { selector: '[data-recording-start] button', label: 1, offsetX: -6, offsetY: -10 },
    { selector: 'form[action="/workflow/play"] button', label: 2, offsetX: -6, offsetY: -10 },
    { selector: 'form[action="/workflow/debug"] button', label: 3, offsetX: -6, offsetY: -10 },
    { selector: '[data-modal-open]', label: 4, offsetX: -6, offsetY: -10 },
    { selector: 'form[action="/workflow/remove"] button', label: 5, offsetX: -6, offsetY: -10 },
    { selector: '[data-status-widget]', label: 6, offsetX: -6, offsetY: -10 }
  ]);
  await page.screenshot({ path: path.join(outDir, 'workflow-controls-real.png'), fullPage: true });

  // Keypad screenshot.
  await page.evaluate(() => {
    const keypad = document.getElementById('keypad');
    if (keypad) {
      keypad.hidden = false;
    }
    if (typeof window.renderKeypad === 'function') {
      window.renderKeypad('keypad');
    }
  });
  await page.waitForSelector('.h3270-keypad:not([hidden])');
  await page.waitForTimeout(400);
  await addBadges(page, [
    { selector: '.h3270-keypad-title', label: 1, offsetX: -6, offsetY: -10 },
    { selector: '.h3270-keypad-mode-btn[data-mode="compact"]', label: 2, offsetX: -6, offsetY: -10 },
    { selector: '.h3270-keypad-hide-btn', label: 3, offsetX: -6, offsetY: -10 },
    { selector: '.h3270-key[data-key="PF1"]', label: 4, offsetX: -6, offsetY: -10 },
    { selector: '.h3270-key[data-key="PA1"]', label: 5, offsetX: -6, offsetY: -10 },
    { selector: '.h3270-key[data-key="Enter"]', label: 6, offsetX: -6, offsetY: -10 }
  ]);
  await page.screenshot({ path: path.join(outDir, 'keypad-real.png'), fullPage: true });

  await browser.close();
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
