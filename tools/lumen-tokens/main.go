// Command lumen-tokens compiles Lumen's tokens.json into tokens.css.
//
// Lumen publishes its design tokens as JSON; the browser needs CSS custom
// properties. The compile rules follow the Lumen design-system format notes:
//
//	:root, [data-theme="<first>"] { colour + shadow custom properties }
//	[data-theme="<other>"]        { per-theme values, aliases re-declared }
//	:root                         { spacing, radius, height, border, icon, font stacks }
//	.<style>                      { one class per type style }
//
// The vendored tokens.json stays an unmodified copy of the Lumen release
// (docs/12, DS-10); only this derived file is generated.
//
// Usage: go run ./tools/lumen-tokens [-dir internal/server/web/vendor/lumen] [-check]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type tokenFile struct {
	Name  string `json:"name"`
	Color struct {
		Themes []struct {
			ID string `json:"id"`
		} `json:"themes"`
		Tokens []colorToken `json:"tokens"`
	} `json:"color"`
	Type struct {
		Families map[string]string `json:"families"`
		Groups   []struct {
			Family string `json:"family"`
			Styles []struct {
				Name          string          `json:"name"`
				FontSize      string          `json:"fontSize"`
				LineHeight    json.RawMessage `json:"lineHeight"`
				FontWeight    json.RawMessage `json:"fontWeight"`
				LetterSpacing string          `json:"letterSpacing"`
			} `json:"styles"`
		} `json:"groups"`
	} `json:"type"`
	Spacing tokenList `json:"spacing"`
	Radius  tokenList `json:"radius"`
	Shadow  struct {
		Tokens []colorToken `json:"tokens"`
	} `json:"shadow"`
	Height tokenList `json:"height"`
	Border tokenList `json:"border"`
	Icon   tokenList `json:"icon"`
}

type tokenList struct {
	Tokens []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"tokens"`
}

type colorToken struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

var aliasRe = regexp.MustCompile(`^\{([A-Za-z0-9][A-Za-z0-9_.-]*)\}$`)

func main() {
	dir := flag.String("dir", "internal/server/web/vendor/lumen", "directory holding tokens.json")
	check := flag.Bool("check", false, "fail if tokens.css is not up to date instead of writing it")
	flag.Parse()

	raw, err := os.ReadFile(filepath.Join(*dir, "tokens.json"))
	if err != nil {
		fatal(err)
	}
	css, err := Compile(raw)
	if err != nil {
		fatal(err)
	}
	out := filepath.Join(*dir, "tokens.css")
	if *check {
		cur, err := os.ReadFile(out)
		if err != nil || !bytes.Equal(cur, css) {
			fatal(fmt.Errorf("%s is out of date: run `go run ./tools/lumen-tokens`", out))
		}
		return
	}
	if err := os.WriteFile(out, css, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", out, len(css))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "lumen-tokens:", err)
	os.Exit(1)
}

// Compile turns the contents of tokens.json into tokens.css.
func Compile(raw []byte) ([]byte, error) {
	var t tokenFile
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse tokens.json: %w", err)
	}
	if len(t.Color.Themes) == 0 {
		return nil, fmt.Errorf("tokens.json has no themes")
	}
	first := t.Color.Themes[0].ID

	var b strings.Builder
	fmt.Fprintf(&b, "/* %s — generated from tokens.json by tools/lumen-tokens. Do not edit. */\n", t.Name)

	for i, th := range t.Color.Themes {
		if i == 0 {
			fmt.Fprintf(&b, ":root, [data-theme=%q] {\n", th.ID)
		} else {
			fmt.Fprintf(&b, "[data-theme=%q] {\n", th.ID)
		}
		all := append(append([]colorToken{}, t.Color.Tokens...), t.Shadow.Tokens...)
		for _, tok := range all {
			v, ok, err := themeValue(tok, th.ID, first, i == 0)
			if err != nil {
				return nil, err
			}
			if ok {
				fmt.Fprintf(&b, "  --%s: %s;\n", tok.Name, v)
			}
		}
		b.WriteString("}\n")
	}

	b.WriteString(":root {\n")
	for _, l := range []tokenList{t.Spacing, t.Radius, t.Height, t.Border, t.Icon} {
		for _, tok := range l.Tokens {
			fmt.Fprintf(&b, "  --%s: %s;\n", tok.Name, tok.Value)
		}
	}
	for _, key := range sortedKeys(t.Type.Families) {
		fmt.Fprintf(&b, "  --font-%s: %s;\n", key, t.Type.Families[key])
	}
	b.WriteString("}\n")

	for _, g := range t.Type.Groups {
		for _, s := range g.Styles {
			fmt.Fprintf(&b, ".%s { font-family: var(--font-%s); font-size: %s;", s.Name, g.Family, s.FontSize)
			if lh := cssNumber(s.LineHeight); lh != "" {
				fmt.Fprintf(&b, " line-height: %s;", lh)
			}
			if fw := cssNumber(s.FontWeight); fw != "" {
				fmt.Fprintf(&b, " font-weight: %s;", fw)
			}
			if s.LetterSpacing != "" {
				fmt.Fprintf(&b, " letter-spacing: %s;", s.LetterSpacing)
			}
			b.WriteString(" }\n")
		}
	}
	return []byte(b.String()), nil
}

// themeValue resolves a token's value for one theme. A token that has no value
// for a later theme inherits the first theme's, which the cascade already does,
// so it is simply omitted from later blocks.
func themeValue(tok colorToken, theme, first string, isFirst bool) (string, bool, error) {
	var s string
	if err := json.Unmarshal(tok.Value, &s); err == nil {
		if !isFirst {
			return "", false, nil
		}
		return cssValue(s), true, nil
	}
	var m map[string]string
	if err := json.Unmarshal(tok.Value, &m); err != nil {
		return "", false, fmt.Errorf("token %s: unsupported value %s", tok.Name, tok.Value)
	}
	v, ok := m[theme]
	if !ok {
		if !isFirst {
			return "", false, nil
		}
		v, ok = m[first]
		if !ok {
			return "", false, fmt.Errorf("token %s: no value for first theme", tok.Name)
		}
	}
	return cssValue(v), true, nil
}

func cssValue(v string) string {
	if m := aliasRe.FindStringSubmatch(v); m != nil {
		return "var(--" + m[1] + ")"
	}
	return v
}

func cssNumber(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
