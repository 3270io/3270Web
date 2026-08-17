// SPDX-License-Identifier: AGPL-3.0-or-later

package webassets

import "embed"

//go:embed web/templates/* web/static/*
var FS embed.FS
