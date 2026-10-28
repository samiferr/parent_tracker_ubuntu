// Package cli implements the `parent-tracker` administration command.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/samiferr/parent_tracker_ubuntu/internal/aggregate"
	"github.com/samiferr/parent_tracker_ubuntu/internal/collector"
	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/storage"
	"github.com/samiferr/parent_tracker_ubuntu/internal/view"
)

// DefaultControlSocket is where a non-root caller finds the service without
// being able to read the (root-only) config file.
const DefaultControlSocket = "/run/parent-tracker/control.sock"

// Account is a real, login-capable local user.
type Account struct {
	Name  string
	UID   int
	Home  string
	Shell string
}

// App carries the command's environment, so tests can drive every command
// without a real terminal, root or desktop.
type App struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Version        string
	Now            func() time.Time
	Loc            *time.Location

	IsRoot       func() bool
	IsTerminal   func() bool
	ReadPassword func() (string, error)
	ListAccounts func() ([]Account, error)
	LookupUser   func(name string) (Account, bool)
	Exec         func(ctx context.Context, name string, args ...string) ([]byte, error)
	OSRelease    string
	ConfigPath   string
	Socket       string // control socket for non-root commands
	Root         string // prefix for system paths such as /usr/share (tests)
	DataDir      string // data directory for a configuration created from the defaults (tests; empty means the built-in default)
	Sessions     func(ctx context.Context) ([]collector.SessionInfo, error)

	in *bufio.Reader
}

// NewApp returns an App wired to the real system.
func NewApp(version string) *App {
	a := &App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Version: version, Now: time.Now, Loc: time.Local,
		OSRelease: "/etc/os-release", ConfigPath: config.DefaultPath, Socket: DefaultControlSocket}
	a.IsRoot = func() bool { return os.Geteuid() == 0 }
	a.IsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	a.ReadPassword = func() (string, error) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	a.ListAccounts = systemAccounts
	a.Sessions = func(ctx context.Context) ([]collector.SessionInfo, error) {
		l, err := collector.ConnectSystem()
		if err != nil {
			return nil, err
		}
		defer l.Close()
		return l.Sessions(ctx)
	}
	a.LookupUser = func(name string) (Account, bool) {
		u, err := user.Lookup(name)
		if err != nil {
			return Account{}, false
		}
		uid, _ := strconv.Atoi(u.Uid)
		return Account{Name: u.Username, UID: uid, Home: u.HomeDir}, true
	}
	a.Exec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	return a
}

func (a *App) reader() *bufio.Reader {
	if a.in == nil {
		a.in = bufio.NewReader(a.Stdin)
	}
	return a.in
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Stdout, format, args...) }
func (a *App) println(args ...any)               { fmt.Fprintln(a.Stdout, args...) }
func (a *App) errorf(format string, args ...any) int {
	fmt.Fprintf(a.Stderr, "parent-tracker: "+format+"\n", args...)
	return 1
}

// ask prints a prompt and reads one line.
func (a *App) ask(prompt string) (string, error) {
	fmt.Fprint(a.Stdout, prompt)
	line, err := a.reader().ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

type command struct {
	name, summary string
	run           func(a *App, args []string) int
}

func commands() []command {
	return []command{
		{"version", "print the version", (*App).cmdVersion},
		{"setup", "choose who is monitored, set the dashboard login, acknowledge the disclosure", (*App).cmdSetup},
		{"status", "show the service, storage and each monitored session", (*App).cmdStatus},
		{"doctor", "diagnose OS support, session types, the app sensor and permissions", (*App).cmdDoctor},
		{"users", "list, add or remove monitored users", (*App).cmdUsers},
		{"config", "list, get or set settings", (*App).cmdConfig},
		{"report", "print a usage report, by day or by hour", (*App).cmdReport},
		{"export", "write everything that is recorded as JSON", (*App).cmdExport},
		{"delete", "permanently delete recorded data", (*App).cmdDelete},
		{"self", "show what is recorded about you (no root needed)", (*App).cmdSelf},
		{"serve", "run the dashboard alone, without collectors", (*App).cmdServe},
		{"notice", "show the notice that activity is recorded", (*App).cmdNotice},
		{"completion", "print a shell completion script", (*App).cmdCompletion},
	}
}

// Run executes the command line and returns the exit status.
func Run(args []string, a *App) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		a.usage(a.Stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] == "--version" || args[0] == "-v" {
		return a.cmdVersion(nil)
	}
	for _, c := range commands() {
		if c.name == args[0] {
			return c.run(a, args[1:])
		}
	}
	fmt.Fprintf(a.Stderr, "parent-tracker: unknown command %q\n\n", args[0])
	a.usage(a.Stderr)
	return 2
}

func (a *App) usage(w io.Writer) {
	fmt.Fprintln(w, "Usage: parent-tracker <command> [options]\n\nCommands:")
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "\nMost commands accept -c, --config <path> (default "+config.DefaultPath+").")
	fmt.Fprintln(w, "Run `parent-tracker <command> -h` for a command's options.")
}

func (a *App) cmdVersion(args []string) int {
	a.printf("parent-tracker %s\n", a.Version)
	return 0
}

// newFlags makes a FlagSet that prints its errors to stderr and has -c/--config.
func (a *App) newFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("parent-tracker "+name, flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	cfg := fs.String("config", a.ConfigPath, "path to config.toml")
	fs.StringVar(cfg, "c", a.ConfigPath, "path to config.toml (short for --config)")
	return fs, cfg
}

// parse parses flags that may appear before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// resetHint says how to start again when the configuration file cannot be used.
const resetHint = "To start again from the defaults (the old file is kept as a backup): sudo parent-tracker setup --reset-config"

// newConfig is the configuration of a fresh install.
func (a *App) newConfig() *config.Config {
	cfg := config.Default()
	if a.DataDir != "" {
		cfg.DataDir = a.DataDir
	}
	return cfg
}

func (a *App) loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	switch {
	case err == nil:
		return cfg, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("there is no configuration at %s. Run `sudo parent-tracker setup` first", path)
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("cannot read %s: run this command with sudo", path)
	}
	return nil, fmt.Errorf("%s: %v\n%s", path, err, resetHint)
}

// loadOrDefault is loadConfig, except a missing file yields the defaults.
func (a *App) loadOrDefault(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	switch {
	case err == nil:
		return cfg, nil
	case errors.Is(err, fs.ErrNotExist):
		return a.newConfig(), nil
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("cannot read %s: run this command with sudo", path)
	}
	return nil, fmt.Errorf("%s: %v\n%s", path, err, resetHint)
}

func (a *App) openRead(cfg *config.Config) (*storage.DB, error) {
	db, err := storage.OpenReadOnly(cfg.DBPath())
	switch {
	case err == nil:
		return db, nil
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("there is no database at %s yet. Start the service first: sudo systemctl enable --now parent-trackerd", cfg.DBPath())
	case errors.Is(err, fs.ErrPermission):
		return nil, fmt.Errorf("cannot read the database: run this command with sudo")
	}
	return nil, err
}

func (a *App) viewer(cfg *config.Config, db *storage.DB) *view.Viewer {
	return &view.Viewer{DB: db, Cfg: func() *config.Config { return cfg }, Loc: a.Loc, Now: a.Now}
}

func (a *App) today() string { return aggregate.DayString(a.Loc, a.Now().UnixMilli()) }

// pickUser resolves --user against the monitored users known to the database;
// with exactly one monitored user it may be omitted.
func (a *App) pickUser(cfg *config.Config, db *storage.DB, name string) (model.User, error) {
	users, err := db.Users()
	if err != nil {
		return model.User{}, err
	}
	var monitored []model.User
	for _, u := range users {
		if u.Monitored && cfg.IsMonitored(u.Username) {
			if name == u.Username {
				return u, nil
			}
			monitored = append(monitored, u)
		}
	}
	switch {
	case name != "":
		return model.User{}, fmt.Errorf("%q is not a monitored user", name)
	case len(monitored) == 1:
		return monitored[0], nil
	case len(monitored) == 0:
		return model.User{}, fmt.Errorf("no user is monitored yet. Run `sudo parent-tracker users add NAME` and start the service")
	}
	names := make([]string, len(monitored))
	for i, u := range monitored {
		names[i] = u.Username
	}
	sort.Strings(names)
	return model.User{}, fmt.Errorf("choose a user with --user (%s)", strings.Join(names, ", "))
}

// controlClient talks to the control socket.
func controlClient(socket string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
}

func socketErr(err error, socket string) error {
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("the service is not running (no answer on %s). Start it with: sudo systemctl start parent-trackerd", socket)
	}
	if errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("cannot open %s: permission denied", socket)
	}
	return err
}
