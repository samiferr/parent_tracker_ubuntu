package osrel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) Info {
	t.Helper()
	p := filepath.Join(t.TempDir(), "os-release")
	os.WriteFile(p, []byte(body), 0o644)
	in, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// AC-64: supported on 24.04 or newer; "unsupported OS" with the upgrade hint
// on 22.04 or older.
func TestJudge(t *testing.T) {
	cases := []struct {
		name, body string
		want       Verdict
		contains   string
	}{
		{"24.04", "ID=ubuntu\nVERSION_ID=\"24.04\"\nPRETTY_NAME=\"Ubuntu 24.04.5 LTS\"\n", Supported, "is supported"},
		{"25.10", "ID=ubuntu\nVERSION_ID=\"25.10\"\nPRETTY_NAME=\"Ubuntu 25.10\"\n", Supported, ""},
		{"26.04", "ID=ubuntu\nVERSION_ID=\"26.04\"\n", Supported, ""},
		{"22.04", "ID=ubuntu\nVERSION_ID=\"22.04\"\nPRETTY_NAME=\"Ubuntu 22.04.4 LTS\"\n", Unsupported, "do-release-upgrade"},
		{"20.04", "ID=ubuntu\nVERSION_ID=\"20.04\"\n", Unsupported, "Unsupported OS"},
		{"23.10", "ID=ubuntu\nVERSION_ID=\"23.10\"\n", Unsupported, "older than Ubuntu 24.04"},
		{"mint on noble", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nUBUNTU_CODENAME=noble\nPRETTY_NAME=\"Linux Mint 22\"\n", BestEffort, "best-effort"},
		{"mint on jammy", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\nUBUNTU_CODENAME=jammy\nPRETTY_NAME=\"Linux Mint 21\"\n", Unsupported, "22.04"},
		{"debian", "ID=debian\nVERSION_ID=\"12\"\nPRETTY_NAME=\"Debian 12\"\n", Unsupported, "Ubuntu 24.04 LTS and newer only"},
		{"garbage", "ID=ubuntu\nVERSION_ID=\"rolling\"\n", Unsupported, "Cannot read"},
	}
	for _, c := range cases {
		v, msg := Judge(write(t, c.body))
		if v != c.want || !strings.Contains(msg, c.contains) {
			t.Errorf("%s: %v %q", c.name, v, msg)
		}
	}
}

func TestReadMissing(t *testing.T) {
	if _, err := Read("/nonexistent/os-release"); err == nil {
		t.Fatal("expected an error")
	}
}
