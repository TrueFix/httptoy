// Package templates contains httptoy's embedded documentation page.
package templates

import "embed"

// Files contains the documentation templates.
//
//go:embed *.html web
var Files embed.FS
