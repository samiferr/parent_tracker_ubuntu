package packaging

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agent"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
)

func read(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The annotated example is what a fresh install gets: it must load under the
// strict parser and say exactly what the built-in defaults say.
func TestExampleConfigMatchesDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte(read(t, "packaging/config.toml.example")))
	if err != nil {
		t.Fatalf("example config does not load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("example config does not validate: %v", err)
	}
	want := config.Default()
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("example config differs from the defaults:\n got  %+v\n want %+v", cfg, want)
	}
}

// The extension and the agent must agree on every name they share.
func TestExtensionMatchesAgent(t *testing.T) {
	var meta struct {
		UUID         string   `json:"uuid"`
		ShellVersion []string `json:"shell-version"`
	}
	if err := json.Unmarshal([]byte(read(t, "gnome-extension/metadata.json")), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.UUID != agent.ExtensionUUID {
		t.Errorf("metadata uuid %q != agent.ExtensionUUID %q", meta.UUID, agent.ExtensionUUID)
	}
	if len(meta.ShellVersion) == 0 {
		t.Error("metadata lists no shell-version")
	}
	js := read(t, "gnome-extension/extension.js")
	for _, want := range []string{
		"'" + agent.SensorName + "'", "'" + string(agent.SensorPath) + "'", `name="` + agent.SensorIface + `"`,
		`method name="GetFocus"`, `method name="SetTitleCapture"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("extension.js does not contain %s", want)
		}
	}
	// GetFocus returns (b, s, s, u, s): available, id, name, pid, title.
	sig := regexp.MustCompile(`<method name="GetFocus">((?s:.*?))</method>`).FindStringSubmatch(js)
	if sig == nil {
		t.Fatal("no GetFocus method in the extension")
	}
	var types []string
	for _, m := range regexp.MustCompile(`type="(\w)" direction="out"`).FindAllStringSubmatch(sig[1], -1) {
		types = append(types, m[1])
	}
	if strings.Join(types, "") != "bssus" {
		t.Errorf("GetFocus returns %q, the agent reads \"bssus\"", strings.Join(types, ""))
	}
	// The extension must stay passive and local (docs/04 §4.13.5).
	for _, banned := range []string{"Soup", "fetch(", "XMLHttpRequest", "GLib.file_set_contents", "Gio.File", "GLib.spawn", "Clutter.get_default_backend", "St.Clipboard", "global.stage.connect"} {
		if strings.Contains(js, banned) {
			t.Errorf("extension.js uses %q; it must have no network, file, process or input access", banned)
		}
	}
}

func TestUnitsAndDesktopFilesPointAtRealThings(t *testing.T) {
	daemon := read(t, "packaging/parent-trackerd.service")
	for _, want := range []string{"Type=notify", "ExecStart=/usr/bin/parent-trackerd --config /etc/parent-tracker/config.toml", "ExecReload=", "NoNewPrivileges=yes", "ProtectSystem=strict", "ReadWritePaths=/etc/parent-tracker"} {
		if !strings.Contains(daemon, want) {
			t.Errorf("parent-trackerd.service lacks %q", want)
		}
	}
	agentUnit := read(t, "packaging/parent-tracker-agent.service")
	if !strings.Contains(agentUnit, "ExecStart=/usr/bin/parent-tracker-agent") || !strings.Contains(agentUnit, "WantedBy=graphical-session.target") {
		t.Error("the agent unit should run /usr/bin/parent-tracker-agent from graphical-session.target")
	}
	for _, bin := range []string{"parent-trackerd", "parent-tracker", "parent-tracker-agent"} {
		if _, err := os.Stat(filepath.Join("..", "cmd", bin, "main.go")); err != nil {
			t.Errorf("no cmd/%s", bin)
		}
	}
	notice := read(t, "packaging/parent-tracker-notice.desktop")
	if !strings.Contains(notice, "Exec=/usr/bin/parent-tracker notice --notify") {
		t.Error("the autostart entry should run `parent-tracker notice --notify`")
	}
	self := read(t, "packaging/parent-tracker-self.desktop")
	if !strings.Contains(self, "Exec=/usr/bin/parent-tracker self") || !strings.Contains(self, "Terminal=true") {
		t.Error("the menu entry should open `parent-tracker self` in a terminal")
	}
	cli := read(t, "internal/cli/cli.go") + read(t, "internal/cli/misc.go")
	for _, cmd := range []string{`"notice"`, `"self"`} {
		if !strings.Contains(cli, cmd) {
			t.Errorf("the CLI has no %s command", cmd)
		}
	}
}

func TestNoticesCoverWhatShips(t *testing.T) {
	n := read(t, "internal/server/web/notices.txt")
	for _, want := range []string{"Lumen", "Spectrum", "Apache License", "Lucide", "Feather", "React", "Source Sans 3", "Source Code Pro", "SIL OPEN FONT LICENSE",
		"modernc.org/sqlite", "github.com/godbus/dbus/v5", "github.com/pelletier/go-toml/v2", "golang.org/x/crypto", "github.com/jezek/xgb", "The Go standard library"} {
		if !strings.Contains(n, want) {
			t.Errorf("notices.txt does not mention %q", want)
		}
	}
	if strings.Contains(n, "placeholder") {
		t.Error("notices.txt is still the placeholder")
	}
}

// fakeGSettings returns a PATH directory with a gsettings that prints out.
func fakeGSettings(t *testing.T, out string, ok bool) string {
	t.Helper()
	dir := t.TempDir()
	body := "#!/bin/sh\n"
	if ok {
		body += "printf '%s\\n' \"" + out + "\"\n"
	} else {
		body += "exit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "gsettings"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runSetup(t *testing.T, root, fakeDir, action string) {
	t.Helper()
	cmd := exec.Command("sh", "gnome-extension-setup", action)
	cmd.Env = []string{"PATH=" + fakeDir + ":/usr/bin:/bin", "PARENT_TRACKER_ROOT=" + root}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gnome-extension-setup %s: %v\n%s", action, err, out)
	}
}

func TestGnomeExtensionSetup(t *testing.T) {
	uuid := agent.ExtensionUUID
	dbFile := func(root string) string { return filepath.Join(root, "etc/dconf/db/local.d/90-parent-tracker") }
	lockFile := func(root string) string { return filepath.Join(root, "etc/dconf/db/local.d/locks/90-parent-tracker") }

	t.Run("adds the extension to the list GNOME would use", func(t *testing.T) {
		root := t.TempDir()
		runSetup(t, root, fakeGSettings(t, "['ubuntu-appindicators@ubuntu.com', 'ubuntu-dock@ubuntu.com']", true), "enable")
		got, err := os.ReadFile(dbFile(root))
		if err != nil {
			t.Fatal(err)
		}
		want := "enabled-extensions=['ubuntu-appindicators@ubuntu.com', 'ubuntu-dock@ubuntu.com', '" + uuid + "']"
		if !strings.Contains(string(got), "[org/gnome/shell]") || !strings.Contains(string(got), want) {
			t.Fatalf("dconf default:\n%s\nwant a line %s", got, want)
		}
		lock, _ := os.ReadFile(lockFile(root))
		if !strings.Contains(string(lock), "/org/gnome/shell/enabled-extensions") {
			t.Fatalf("enabled-extensions is not locked:\n%s", lock)
		}
		profile, _ := os.ReadFile(filepath.Join(root, "etc/dconf/profile/user"))
		if string(profile) != "user-db:user\nsystem-db:local\n" {
			t.Fatalf("profile = %q", profile)
		}
	})
	t.Run("an empty list becomes just this extension", func(t *testing.T) {
		root := t.TempDir()
		runSetup(t, root, fakeGSettings(t, "@as []", true), "enable")
		got, _ := os.ReadFile(dbFile(root))
		if !strings.Contains(string(got), "enabled-extensions=['"+uuid+"']") {
			t.Fatalf("got:\n%s", got)
		}
	})
	t.Run("running again does not add it twice", func(t *testing.T) {
		root := t.TempDir()
		runSetup(t, root, fakeGSettings(t, "['a@b', '"+uuid+"']", true), "enable")
		got, _ := os.ReadFile(dbFile(root))
		if strings.Count(string(got), uuid) != 1 {
			t.Fatalf("uuid appears more than once:\n%s", got)
		}
	})
	t.Run("keeps an existing profile and appends the system database", func(t *testing.T) {
		root := t.TempDir()
		p := filepath.Join(root, "etc/dconf/profile/user")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("user-db:user\nsystem-db:site\n"), 0o644)
		runSetup(t, root, fakeGSettings(t, "[]", true), "enable")
		got, _ := os.ReadFile(p)
		if string(got) != "user-db:user\nsystem-db:site\nsystem-db:local\n" {
			t.Fatalf("profile = %q", got)
		}
		runSetup(t, root, fakeGSettings(t, "[]", true), "enable")
		got, _ = os.ReadFile(p)
		if strings.Count(string(got), "system-db:local") != 1 {
			t.Fatalf("system-db:local repeated: %q", got)
		}
	})
	t.Run("does nothing without GNOME Shell", func(t *testing.T) {
		root := t.TempDir()
		runSetup(t, root, fakeGSettings(t, "", false), "enable") // gsettings fails: no schema
		if _, err := os.Stat(dbFile(root)); err == nil {
			t.Fatal("wrote a dconf default on a system without GNOME Shell")
		}
		root = t.TempDir()
		runSetup(t, root, t.TempDir(), "enable") // PATH still has /usr/bin; the real gsettings, if any, has no GNOME Shell schema here
		if _, err := os.Stat(dbFile(root)); err == nil {
			t.Fatal("wrote a dconf default without a GNOME Shell schema")
		}
	})
	t.Run("disable removes the default and the lock", func(t *testing.T) {
		root := t.TempDir()
		fake := fakeGSettings(t, "['a@b']", true)
		runSetup(t, root, fake, "enable")
		runSetup(t, root, fake, "disable")
		for _, f := range []string{dbFile(root), lockFile(root)} {
			if _, err := os.Stat(f); err == nil {
				t.Errorf("%s still exists", f)
			}
		}
	})
}

// TestPackageLifecycle builds the real .deb from stub binaries, unpacks it into
// a scratch root and runs the maintainer scripts the way dpkg does (chrootless,
// DPKG_ROOT set): install, upgrade, remove and purge.
func TestPackageLifecycle(t *testing.T) {
	for _, tool := range []string{"dpkg-deb", "go", "systemctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	work := t.TempDir()
	bin := filepath.Join(work, "bin")
	os.MkdirAll(bin, 0o755)
	for _, b := range []string{"parent-trackerd", "parent-tracker", "parent-tracker-agent"} {
		os.WriteFile(filepath.Join(bin, b), []byte("#!/bin/sh\n"), 0o755)
	}
	repo, _ := filepath.Abs("..")
	build := exec.Command("sh", "packaging/build-deb.sh", "v1.2.3-4-gabc1234-dirty", "arm64", bin)
	build.Dir = repo
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build-deb.sh: %v\n%s", err, out)
	}
	deb := filepath.Join(repo, strings.TrimSpace(string(out)))
	t.Cleanup(func() { os.Remove(deb) })
	if !strings.HasSuffix(deb, "parent-tracker_1.2.3+4.gabc1234+dirty_arm64.deb") {
		t.Fatalf("package name %q: the git version was not made Debian-valid", deb)
	}

	control, err := exec.Command("dpkg-deb", "-f", deb).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Package: parent-tracker", "Architecture: arm64", "Depends: systemd (>= 255)"} {
		if !strings.Contains(string(control), want) {
			t.Errorf("control lacks %q:\n%s", want, control)
		}
	}

	root := filepath.Join(work, "root")
	os.MkdirAll(root, 0o755)
	if out, err := exec.Command("dpkg-deb", "-x", deb, root).CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb -x: %v\n%s", err, out)
	}
	ctl := filepath.Join(work, "ctl")
	if out, err := exec.Command("dpkg-deb", "-e", deb, ctl).CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb -e: %v\n%s", err, out)
	}
	script := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(filepath.Join(ctl, name), args...)
		cmd.Env = []string{"PATH=" + fakeGSettings(t, "['ubuntu-dock@ubuntu.com']", true) + ":/usr/bin:/bin", "DPKG_ROOT=" + root}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	exists := func(rel string) bool { _, err := os.Lstat(filepath.Join(root, rel)); return err == nil }

	for _, rel := range []string{
		"usr/bin/parent-trackerd", "usr/bin/parent-tracker", "usr/bin/parent-tracker-agent",
		"usr/lib/systemd/system/parent-trackerd.service", "usr/lib/systemd/user/parent-tracker-agent.service",
		"usr/lib/tmpfiles.d/parent-tracker.conf", "usr/lib/parent-tracker/gnome-extension-setup",
		"usr/share/gnome-shell/extensions/" + agent.ExtensionUUID + "/metadata.json",
		"usr/share/gnome-shell/extensions/" + agent.ExtensionUUID + "/extension.js",
		"etc/xdg/autostart/parent-tracker-notice.desktop", "usr/share/applications/parent-tracker-self.desktop",
		"usr/share/doc/parent-tracker/copyright", "usr/share/doc/parent-tracker/config.toml.example",
		"usr/share/bash-completion/completions/parent-tracker",
	} {
		if !exists(rel) {
			t.Errorf("the package does not contain %s", rel)
		}
	}
	if exists("etc/parent-tracker/config.toml") {
		t.Error("the package ships /etc/parent-tracker/config.toml; it must be created by postinst so upgrades never replace it")
	}

	// Fresh install.
	msg := script("postinst", "configure")
	if !strings.Contains(msg, "parent-tracker setup") {
		t.Errorf("first install should say what to do next, got:\n%s", msg)
	}
	cfgPath := filepath.Join(root, "etc/parent-tracker/config.toml")
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("postinst did not create the config: %v", err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("config mode = %o, want 640", st.Mode().Perm())
	}
	if _, err := config.Load(cfgPath); err != nil {
		t.Errorf("installed config does not load: %v", err)
	}
	if !exists("etc/systemd/system/multi-user.target.wants/parent-trackerd.service") {
		t.Error("the daemon is not enabled")
	}
	if !exists("etc/systemd/user/graphical-session.target.wants/parent-tracker-agent.service") {
		t.Error("the agent is not enabled for all users")
	}
	if !exists("etc/dconf/db/local.d/90-parent-tracker") || !exists("etc/dconf/db/local.d/locks/90-parent-tracker") {
		t.Error("the GNOME extension was not enabled and locked through dconf")
	}

	// Upgrade: the administrator's edits survive and nothing is repeated.
	if err := os.WriteFile(cfgPath, []byte("monitored_users = [\"kid\"]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if msg := script("postinst", "configure", "0.9.0"); strings.Contains(msg, "Next steps") {
		t.Errorf("an upgrade should not repeat the first-install instructions:\n%s", msg)
	}
	if b, _ := os.ReadFile(cfgPath); !strings.Contains(string(b), "kid") {
		t.Error("an upgrade replaced the administrator's configuration")
	}

	// Remove: units and dconf lock go away, the data stays.
	os.MkdirAll(filepath.Join(root, "var/lib/parent-tracker"), 0o700)
	os.WriteFile(filepath.Join(root, "var/lib/parent-tracker/tracker.db"), []byte("data"), 0o600)
	script("prerm", "remove")
	script("postrm", "remove")
	if exists("etc/systemd/system/multi-user.target.wants/parent-trackerd.service") || exists("etc/systemd/user/graphical-session.target.wants/parent-tracker-agent.service") {
		t.Error("remove left the units enabled")
	}
	if exists("etc/dconf/db/local.d/90-parent-tracker") {
		t.Error("remove left the extension default and lock in place")
	}
	if !exists("etc/parent-tracker/config.toml") || !exists("var/lib/parent-tracker/tracker.db") {
		t.Error("remove deleted configuration or data; only purge may")
	}

	// Purge: everything the service wrote is deleted.
	script("postrm", "purge")
	for _, rel := range []string{"etc/parent-tracker", "var/lib/parent-tracker"} {
		if exists(rel) {
			t.Errorf("purge left %s", rel)
		}
	}
}
