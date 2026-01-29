# Palette's Journal

## 2025-05-18 - Missing Feedback on File Uploads
**Learning:** File inputs triggered by custom buttons often lack immediate feedback. When a user selects a file, the browser might not show any indication of activity until the server responds, leading to uncertainty ("Did I click it?").
**Action:** Always attach a loading state (spinner/text change) immediately upon the `change` event of the file input, before the form submission occurs.

## 2025-05-18 - Non-Semantic Modals
**Learning:** The application uses `div` elements with `hidden` attributes for modals (e.g., sample app selection) but lacks semantic ARIA roles (`dialog`, `aria-modal`) and label associations, making them invisible or confusing to screen reader users.
**Action:** When working on modals in this codebase, always manually add `role="dialog"`, `aria-modal="true"`, and `aria-labelledby` to ensure accessibility, as the base implementation does not include them.

## 2025-05-18 - Manual Focus Management in Vanilla Modals
**Learning:** Standard HTML/CSS modals (using `hidden` attribute) do not automatically manage focus. Explicit JavaScript is required to save the previous focus, move focus into the modal on open, and restore it on close.
**Action:** When implementing or modifying vanilla JS modals, always add focus management logic to ensure keyboard accessibility.

## 2026-01-28 - Icon-Only Button Loading States
**Learning:** Standard loading patterns (injecting "Loading..." text) break layout on icon-only buttons with locked widths, causing overflow and visual glitches.
**Action:** For icon-only buttons (`.icon-button`), only show the spinner (removing margins) and use `aria-label="Loading..."` to convey state without changing dimensions or layout.
