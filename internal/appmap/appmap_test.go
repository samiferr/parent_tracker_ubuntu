package appmap

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	sys, user := t.TempDir(), t.TempDir()
	write(t, sys, "firefox.desktop", "[Desktop Entry]\nType=Application\nName=Firefox Web Browser\nName[fr]=Navigateur\nExec=firefox %u\nStartupWMClass=Navigator\n")
	write(t, sys, "org.gnome.Nautilus.desktop", "[Desktop Entry]\nType=Application\nName=Files\nExec=nautilus --new-window %U\n")
	write(t, sys, "code.desktop", "[Desktop Entry]\nType=Application\nName=Visual Studio Code\nExec=env GDK_BACKEND=x11 /usr/share/code/code %F\nStartupWMClass=Code\n")
	write(t, sys, "kde/dolphin.desktop", "[Desktop Entry]\nType=Application\nName=Dolphin\nExec=dolphin\n")
	write(t, sys, "link.desktop", "[Desktop Entry]\nType=Link\nName=Not an app\n")
	write(t, user, "firefox.desktop", "[Desktop Entry]\nType=Application\nName=My Firefox\nExec=firefox\n")
	ix := New(user, sys)

	cases := []struct {
		in       []string
		id, name string
	}{
		{[]string{"firefox"}, "firefox", "My Firefox"},                         // earlier directories win
		{[]string{"", "Navigator"}, "firefox", "My Firefox"},                   // StartupWMClass
		{[]string{"org.gnome.Nautilus"}, "org.gnome.nautilus", "Files"},        // GTK application id
		{[]string{"unknown-class", "nautilus"}, "org.gnome.nautilus", "Files"}, // by executable
		{[]string{"Code"}, "code", "Visual Studio Code"},
		{[]string{"firefox_firefox.desktop"}, "firefox_firefox", "firefox_firefox"}, // no entry: still tracked
		{[]string{"kde-dolphin"}, "kde-dolphin", "Dolphin"},
		{[]string{"Not an app"}, "not an app", "Not an app"},
		{[]string{"", " "}, "", ""},
	}
	for _, c := range cases {
		id, name := ix.Resolve(c.in...)
		if id != c.id || name != c.name {
			t.Errorf("Resolve(%q) = %q, %q; want %q, %q", c.in, id, name, c.id, c.name)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize(" Firefox_Firefox.desktop "); got != "firefox_firefox" {
		t.Fatal(got)
	}
}

func TestExecName(t *testing.T) {
	for in, want := range map[string]string{
		"firefox %u": "firefox", "env A=1 /usr/bin/vlc --x": "vlc", `"/opt/My App/run" -x`: "run", "": "",
	} {
		if got := execName(in); got != want {
			t.Errorf("execName(%q) = %q, want %q", in, got, want)
		}
	}
}
