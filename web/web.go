// Package web embeds the HTML templates and static files for the web view.
package web

import "embed"

// FS holds templates/*.html and static/*.
//
//go:embed templates static
var FS embed.FS
