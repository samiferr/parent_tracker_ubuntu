// Package osrel reads /etc/os-release and judges whether the OS is supported
// (docs/04 §4.10: Ubuntu 24.04 LTS and newer only).
package osrel

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Info is the parsed os-release file.
type Info struct {
	ID, VersionID, Name, PrettyName string
	IDLike, UbuntuCodename          string
}

// Verdict is the support level of an OS.
type Verdict int

const (
	// Supported: Ubuntu 24.04 or newer.
	Supported Verdict = iota
	// BestEffort: an Ubuntu flavour or derivative based on 24.04 or newer.
	BestEffort
	// Unsupported: Ubuntu older than 24.04, or an unrelated OS.
	Unsupported
)

// MinMajor and MinMinor are the oldest supported Ubuntu release.
const (
	MinMajor = 24
	MinMinor = 4
)

// Read parses an os-release file.
func Read(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	var in Info
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			in.ID = v
		case "VERSION_ID":
			in.VersionID = v
		case "NAME":
			in.Name = v
		case "PRETTY_NAME":
			in.PrettyName = v
		case "ID_LIKE":
			in.IDLike = v
		case "UBUNTU_CODENAME":
			in.UbuntuCodename = v
		}
	}
	return in, sc.Err()
}

// codenames maps Ubuntu codenames of releases from 22.04 on to their versions,
// so a derivative that only names its Ubuntu base can be judged (docs/04 §4.10).
var codenames = map[string][2]int{
	"focal": {20, 4}, "jammy": {22, 4}, "kinetic": {22, 10}, "lunar": {23, 4}, "mantic": {23, 10},
	"noble": {24, 4}, "oracular": {24, 10}, "plucky": {25, 4}, "questing": {25, 10}, "resolute": {26, 4},
}

func parseVersion(v string) (major, minor int, ok bool) {
	a, b, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(a)
	minor, err2 := strconv.Atoi(strings.Fields(b + " ")[0])
	return major, minor, err1 == nil && err2 == nil
}

func atLeast(major, minor int) bool {
	return major > MinMajor || (major == MinMajor && minor >= MinMinor)
}

// Judge says whether the OS is supported and, if not, what to do.
func Judge(in Info) (Verdict, string) {
	name := in.PrettyName
	if name == "" {
		name = in.Name + " " + in.VersionID
	}
	if in.ID == "ubuntu" {
		major, minor, ok := parseVersion(in.VersionID)
		if !ok {
			return Unsupported, fmt.Sprintf("Cannot read the Ubuntu version from %q.", name)
		}
		if atLeast(major, minor) {
			return Supported, name + " is supported."
		}
		return Unsupported, fmt.Sprintf("Unsupported OS: %s is older than Ubuntu 24.04. Upgrade to 24.04 or newer with `sudo do-release-upgrade`.", name)
	}
	if strings.Contains(in.IDLike, "ubuntu") && in.UbuntuCodename != "" {
		if v, ok := codenames[in.UbuntuCodename]; ok {
			if atLeast(v[0], v[1]) {
				return BestEffort, fmt.Sprintf("%s is based on Ubuntu %d.%02d. Flavours and derivatives are supported on a best-effort basis.", name, v[0], v[1])
			}
			return Unsupported, fmt.Sprintf("Unsupported OS: %s is based on Ubuntu %d.%02d, which is older than 24.04. Upgrade it to a release based on 24.04 or newer.", name, v[0], v[1])
		}
		return BestEffort, fmt.Sprintf("%s is an Ubuntu derivative (%s); support is best effort.", name, in.UbuntuCodename)
	}
	return Unsupported, fmt.Sprintf("Unsupported OS: %s. Parent Tracker supports Ubuntu 24.04 LTS and newer only.", name)
}
