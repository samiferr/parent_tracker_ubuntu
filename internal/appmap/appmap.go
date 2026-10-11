// Package appmap maps windows and processes to human-readable application
// names using the desktop entries installed on the machine (FR-3.3).
package appmap

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Index is a lookup built from .desktop files.
type Index struct {
	mu     sync.Mutex
	dirs   []string
	built  time.Time
	byID   map[string]entry // desktop file id (lower, no .desktop)
	byWM   map[string]entry // StartupWMClass (lower)
	byExec map[string]entry // basename of Exec (lower)
	ttl    time.Duration
}

type entry struct{ id, name string }

// DefaultDirs lists the usual application directories for the current user.
func DefaultDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" && home != "" {
		data = filepath.Join(home, ".local", "share")
	}
	if data != "" {
		dirs = append(dirs, filepath.Join(data, "applications"), filepath.Join(data, "flatpak", "exports", "share", "applications"))
	}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(sys, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return append(dirs, "/var/lib/flatpak/exports/share/applications", "/var/lib/snapd/desktop/applications")
}

// New builds an index over dirs (DefaultDirs when empty). It is rebuilt lazily
// every few minutes so newly installed applications are found.
func New(dirs ...string) *Index {
	if len(dirs) == 0 {
		dirs = DefaultDirs()
	}
	return &Index{dirs: dirs, ttl: 5 * time.Minute}
}

func (ix *Index) build() {
	byID, byWM, byExec := map[string]entry{}, map[string]entry{}, map[string]entry{}
	// earlier directories win (user entries override system ones)
	for _, dir := range ix.dirs {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, p)
			id := strings.ToLower(strings.TrimSuffix(strings.ReplaceAll(rel, string(filepath.Separator), "-"), ".desktop"))
			f := parse(p)
			if f.name == "" || f.typ != "Application" {
				return nil
			}
			e := entry{id: id, name: f.name}
			if _, ok := byID[id]; !ok {
				byID[id] = e
			}
			if f.wm != "" {
				if _, ok := byWM[strings.ToLower(f.wm)]; !ok {
					byWM[strings.ToLower(f.wm)] = e
				}
			}
			if f.exec != "" {
				if _, ok := byExec[f.exec]; !ok {
					byExec[f.exec] = e
				}
			}
			return nil
		})
	}
	ix.byID, ix.byWM, ix.byExec, ix.built = byID, byWM, byExec, time.Now()
}

type desktopFile struct{ name, wm, exec, typ string }

func parse(path string) desktopFile {
	f, err := os.Open(path)
	if err != nil {
		return desktopFile{}
	}
	defer f.Close()
	var d desktopFile
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry || line == "" || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k { // unlocalised keys only
		case "Name":
			d.name = v
		case "StartupWMClass":
			d.wm = v
		case "Type":
			d.typ = v
		case "Exec":
			d.exec = execName(v)
		}
	}
	return d
}

// execName is the lower-cased basename of the program in an Exec= line,
// skipping a leading "env" and VAR=value assignments and honouring quotes.
func execName(exec string) string {
	toks := splitQuoted(exec)
	for len(toks) > 0 && (toks[0] == "env" || strings.Contains(toks[0], "=")) {
		toks = toks[1:]
	}
	if len(toks) == 0 {
		return ""
	}
	return strings.ToLower(filepath.Base(toks[0]))
}

// splitQuoted splits on spaces, treating "double quoted" text as one token.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote, has = !inQuote, true
		case r == ' ' && !inQuote:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// Resolve returns the canonical application id and display name for the first
// candidate (a GTK application id, a WM_CLASS, an executable name, ...) that
// matches a desktop entry. With no match it falls back to the first non-empty
// candidate, lower-cased, so that an app is never silently dropped.
func (ix *Index) Resolve(candidates ...string) (id, name string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.byID == nil || time.Since(ix.built) > ix.ttl {
		ix.build()
	}
	first := ""
	for _, c := range candidates {
		c = strings.TrimSpace(strings.TrimSuffix(c, ".desktop"))
		if c == "" {
			continue
		}
		if first == "" {
			first = c
		}
		l := strings.ToLower(c)
		for _, m := range []map[string]entry{ix.byID, ix.byWM, ix.byExec} {
			if e, ok := m[l]; ok {
				if canon, ok := ix.byID[e.id]; ok { // one name per id, whichever way it was found
					e = canon
				}
				return e.id, e.name
			}
		}
	}
	if first == "" {
		return "", ""
	}
	return strings.ToLower(first), first
}

// Normalize turns a desktop id such as "firefox_firefox.desktop" into the form
// used as an application id.
func Normalize(desktopID string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(desktopID), ".desktop"))
}
