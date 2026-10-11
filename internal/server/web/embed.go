// Package web embeds the dashboard front end: React 18 and Lumen (vendored,
// unmodified), the self-hosted fonts, and this project's own HTML, CSS and JS.
// Nothing here is fetched from another origin (docs/12, DS-9).
package web

import "embed"

// FS holds the dashboard files, rooted at this directory.
//
//go:embed index.html favicon.svg notices.txt css js vendor
var FS embed.FS
