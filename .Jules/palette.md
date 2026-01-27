# Palette's Journal

## 2025-05-18 - Missing Feedback on File Uploads
**Learning:** File inputs triggered by custom buttons often lack immediate feedback. When a user selects a file, the browser might not show any indication of activity until the server responds, leading to uncertainty ("Did I click it?").
**Action:** Always attach a loading state (spinner/text change) immediately upon the `change` event of the file input, before the form submission occurs.

## 2025-05-18 - Non-Semantic Modals
**Learning:** The application uses `div` elements with `hidden` attributes for modals (e.g., sample app selection) but lacks semantic ARIA roles (`dialog`, `aria-modal`) and label associations, making them invisible or confusing to screen reader users.
**Action:** When working on modals in this codebase, always manually add `role="dialog"`, `aria-modal="true"`, and `aria-labelledby` to ensure accessibility, as the base implementation does not include them.
