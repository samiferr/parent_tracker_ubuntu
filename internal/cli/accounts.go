package cli

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// loginDefs reads UID_MIN and UID_MAX from /etc/login.defs (1000 and 60000 when
// unset), the bounds of ordinary user accounts.
func loginDefs(path string) (min, max int) {
	min, max = 1000, 60000
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if n, err := strconv.Atoi(fields[1]); err == nil {
			switch fields[0] {
			case "UID_MIN":
				min = n
			case "UID_MAX":
				max = n
			}
		}
	}
	return
}

// systemAccounts lists the machine's real user accounts: UID within
// UID_MIN..UID_MAX, a login shell, and a home directory. System and service
// accounts, root and nobody are left out automatically.
func systemAccounts() ([]Account, error) {
	return accountsFrom("/etc/passwd", "/etc/login.defs")
}

func accountsFrom(passwd, defs string) ([]Account, error) {
	min, max := loginDefs(defs)
	f, err := os.Open(passwd)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Account
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Split(sc.Text(), ":")
		if len(p) < 7 || strings.HasPrefix(sc.Text(), "#") {
			continue
		}
		uid, err := strconv.Atoi(p[2])
		if err != nil || uid < min || uid > max || p[0] == "nobody" || p[0] == "root" {
			continue
		}
		shell := p[6]
		if shell == "" || strings.HasSuffix(shell, "/nologin") || strings.HasSuffix(shell, "/false") {
			continue
		}
		if st, err := os.Stat(p[5]); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, Account{Name: p[0], UID: uid, Home: p[5], Shell: shell})
	}
	return out, sc.Err()
}

// isOrdinary reports whether a user name is a real account (not a system one).
func (a *App) isOrdinary(name string) (Account, bool) {
	accts, err := a.ListAccounts()
	if err == nil {
		for _, ac := range accts {
			if ac.Name == name {
				return ac, true
			}
		}
	}
	return Account{}, false
}
