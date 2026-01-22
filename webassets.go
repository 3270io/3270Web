package webassets

import "embed"

//go:embed web/templates/* web/static/*
var FS embed.FS
