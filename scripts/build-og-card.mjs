/**
 * Render brand/og-card.html to the social preview PNG the site advertises in
 * its `og:image` / `twitter:image` tags.
 *
 * Usage:
 *   npm i playwright && npx playwright install chromium   # once
 *   node scripts/build-og-card.mjs
 *
 * Options (positional, all optional):
 *   node scripts/build-og-card.mjs <card.html> <out.png> <scale>
 *
 * `scale` is the device pixel ratio to capture at, default 2. The copy served
 * by the app itself is built at 1 — it ships inside the binary, and 300 KB of
 * retina detail is not worth carrying for a card that is only ever posted into
 * a team chat:
 *
 *   node scripts/build-og-card.mjs brand/og-card.html web/static/og-image.png 1
 *
 * Why a script and not `chromium --headless --screenshot`: the plain CLI sizes
 * its capture from the *window*, not the viewport, and headless still reserves
 * space for the (absent) browser frame — a `--window-size=1200,630` run paints
 * a 1200x543 viewport into a 1200x630 image, so the bottom of the card comes
 * out blank. Playwright sets the viewport itself, which is the only way to get
 * exactly 1200x630 of card.
 *
 * The card is captured at deviceScaleFactor 2. The tags advertise 1200x630 and
 * that is the size every scraper lays out; the extra pixels exist so the type
 * stays crisp on the retina phone screens where most links are opened.
 */
import { chromium } from 'playwright'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const card = path.resolve(REPO, process.argv[2] ?? 'brand/og-card.html')
const out = path.resolve(REPO, process.argv[3] ?? 'docs/assets/og-image.png')

const WIDTH = 1200
const HEIGHT = 630
const scale = Number(process.argv[4] ?? 2)

const browser = await chromium.launch()
const page = await browser.newPage({
  viewport: { width: WIDTH, height: HEIGHT },
  deviceScaleFactor: scale,
})

await page.goto(pathToFileURL(card).href, { waitUntil: 'load' })
// Webfonts are a progressive enhancement here — the card renders with the
// locally installed Inter / JetBrains Mono when the network is unavailable —
// but when they *are* being fetched, capturing before they land would bake the
// fallback letterforms into the PNG.
await page.evaluate(() => document.fonts.ready)

await page.screenshot({ path: out, clip: { x: 0, y: 0, width: WIDTH, height: HEIGHT } })
await browser.close()

console.log(`${path.relative(REPO, out)} — ${WIDTH}x${HEIGHT} @${scale}x`)
