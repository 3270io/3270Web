// SPDX-License-Identifier: AGPL-3.0-or-later

package agent

import "embed"

// builtinFS carries the skills and instructions that ship with 3270Web.
//
// Embedding them rather than installing them beside the binary means a bare
// download is complete: there is no state where the assistant is running but
// has no idea how to explore an application. Loose files and extensions layer
// on top, they do not replace this.
//
//go:embed builtin/skills/*/SKILL.md builtin/instructions/*.instructions.md
var builtinFS embed.FS
