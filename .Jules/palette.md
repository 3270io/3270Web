# Palette's Journal

## 2025-05-18 - Missing Feedback on File Uploads
**Learning:** File inputs triggered by custom buttons often lack immediate feedback. When a user selects a file, the browser might not show any indication of activity until the server responds, leading to uncertainty ("Did I click it?").
**Action:** Always attach a loading state (spinner/text change) immediately upon the `change` event of the file input, before the form submission occurs.
