package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// ownFiles are this project's own front-end files (everything except the
// vendored third-party code and the licence notices).
func ownFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(p, "vendor/") || p == "notices.txt" {
			return nil
		}
		b, err := fs.ReadFile(FS, p)
		if err != nil {
			return err
		}
		out[p] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 10 {
		t.Fatalf("expected the dashboard's own files, found %d", len(out))
	}
	return out
}

// DS-1: colours, type, spacing and radii come from Lumen tokens only.
func TestNoHardCodedColours(t *testing.T) {
	colour := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\b(rgb|rgba|hsl|hsla|oklch|lab|lch)\(`)
	for name, src := range ownFiles(t) {
		if name == "favicon.svg" { // an icon cannot reference CSS variables when loaded as an image
			continue
		}
		// "#" followed by hex-like text also appears in anchors and ids; only
		// flag it in CSS and in inline styles.
		if !strings.HasSuffix(name, ".css") && !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".html") {
			continue
		}
		for _, m := range colour.FindAllString(src, -1) {
			if strings.HasPrefix(m, "#") && !strings.HasSuffix(name, ".css") {
				continue
			}
			t.Errorf("%s: hard-coded colour %q; use a Lumen token", name, m)
		}
	}
}

func TestSpacingTypeAndRadiusUseTokens(t *testing.T) {
	px := regexp.MustCompile(`:\s*-?[0-9.]+px\b`)
	fontSize := regexp.MustCompile(`font-size\s*:\s*[0-9]`)
	fontWeight := regexp.MustCompile(`font-weight\s*:\s*[0-9]`)
	radius := regexp.MustCompile(`border-radius\s*:\s*[0-9]`)
	for name, src := range ownFiles(t) {
		if !strings.HasSuffix(name, ".css") {
			continue
		}
		for _, re := range []*regexp.Regexp{px, fontSize, fontWeight, radius} {
			if m := re.FindString(src); m != "" {
				t.Errorf("%s: %q is a raw design value; use the matching Lumen token", name, m)
			}
		}
	}
}

// Palette steps (blue-900, gray-600 ...) are for charts and illustration only.
func TestPaletteStepsOnlyInCharts(t *testing.T) {
	palette := regexp.MustCompile(`var\(--(gray|blue|red|orange|yellow|chartreuse|celery|green|seafoam|cyan|indigo|purple|fuchsia|magenta|pink|brown|cinnamon|silver)-[0-9]+`)
	for name, src := range ownFiles(t) {
		if name == "css/charts.css" {
			continue
		}
		if m := palette.FindString(src); m != "" {
			t.Errorf("%s: palette step %q outside css/charts.css; use a semantic token", name, m)
		}
	}
}

// Nothing is fetched from another origin (no CDN, no web fonts, no analytics).
func TestNoExternalReferences(t *testing.T) {
	url := regexp.MustCompile(`(?:src|href|url\(|import\s|from\s|fetch\()\s*["']?\s*(https?:)?//`)
	for name, src := range ownFiles(t) {
		if loc := url.FindString(src); loc != "" {
			t.Errorf("%s: external reference %q", name, loc)
		}
	}
	html := ownFiles(t)["index.html"]
	for _, bad := range []string{"fonts.googleapis.com", "cdn.", "unpkg.com", "jsdelivr"} {
		if strings.Contains(html, bad) {
			t.Errorf("index.html mentions %q", bad)
		}
	}
	for _, need := range []string{"vendor/lumen/bundle.js", "vendor/react/react.production.min.js", "vendor/fonts/fonts.css", "js/theme-init.js"} {
		if !strings.Contains(html, need) {
			t.Errorf("index.html should load %s", need)
		}
	}
}

// Copy rules: no emoji, no exclamation marks in text the person reads.
func TestCopyRules(t *testing.T) {
	for name, src := range ownFiles(t) {
		if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".html") {
			continue
		}
		for _, r := range src {
			if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF && !unicode.IsLetter(r)) {
				t.Errorf("%s: emoji or pictograph %q", name, r)
				break
			}
		}
		// A string literal that contains "!" followed by a quote, space-free end
		// of a sentence: "Saved!" / 'Welcome!'.
		if m := regexp.MustCompile(`[A-Za-z]!['"` + "`" + `]`).FindString(src); m != "" {
			t.Errorf("%s: exclamation mark in copy near %q", name, m)
		}
		// Forbidden button words.
		for _, bad := range []string{`'OK'`, `'Submit'`, `"OK"`, `"Submit"`} {
			if strings.Contains(src, bad) {
				t.Errorf("%s: avoid the button label %s", name, bad)
			}
		}
	}
}

// Both themes are driven by the data-theme attribute set before first paint.
func TestThemeInit(t *testing.T) {
	files := ownFiles(t)
	init := files["js/theme-init.js"]
	for _, want := range []string{"data-theme", "prefers-color-scheme", "pt-theme"} {
		if !strings.Contains(init, want) {
			t.Errorf("theme-init.js should mention %q", want)
		}
	}
	tokens, err := fs.ReadFile(FS, "vendor/lumen/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tokens), `[data-theme="dark"]`) && !strings.Contains(string(tokens), "[data-theme=dark]") {
		t.Error("generated tokens.css has no dark theme block")
	}
}

// The dashboard's CSP forbids inline scripts, so index.html must not have any.
func TestNoInlineScripts(t *testing.T) {
	html := ownFiles(t)["index.html"]
	inline := regexp.MustCompile(`<script(?:\s[^>]*)?>[^<\s]`)
	if inline.MatchString(html) {
		t.Error("index.html has an inline script; the CSP only allows script-src 'self'")
	}
	if strings.Contains(html, " onclick=") || strings.Contains(html, " onload=") {
		t.Error("index.html has an inline event handler")
	}
}
