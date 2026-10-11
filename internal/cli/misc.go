package cli

import (
	"context"
	"io"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/daemon"
	"github.com/samiferr/parent_tracker_ubuntu/internal/demo"
)

// cmdServe runs the dashboard on its own: no collectors, no sockets. With
// --demo it serves a throwaway database of made-up data, for trying the
// dashboard or checking a change to it.
func (a *App) cmdServe(args []string) int {
	fs, cfgPath := a.newFlags("serve")
	demoMode := fs.Bool("demo", false, "serve made-up demo data from a temporary database (nothing real)")
	listen := fs.String("listen", "", "demo only: address to listen on (default 127.0.0.1:8787)")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(a.Stderr, nil))

	if *demoMode {
		if err := demo.Run(ctx, demo.Options{Listen: *listen, Version: a.Version, Log: log, Out: a.Stdout}); err != nil {
			return a.errorf("%v", err)
		}
		return 0
	}
	cfg, err := a.loadConfig(*cfgPath)
	if err != nil {
		return a.errorf("%v", err)
	}
	_ = cfg
	d, err := daemon.New(daemon.Options{ConfigPath: *cfgPath, Version: a.Version, Log: log, NoCollectors: true, Loc: a.Loc,
		Ready: func(addr string) {
			a.printf("Dashboard (no collectors) on http://%s — press Ctrl+C to stop.\n", addr)
		}})
	if err != nil {
		return a.errorf("%v", err)
	}
	if err := d.Run(ctx); err != nil {
		return a.errorf("%v", err)
	}
	return 0
}

// cmdNotice prints the notice that activity is recorded, and with --notify
// shows it as a desktop notification — only to monitored users (SEC-2).
func (a *App) cmdNotice(args []string) int {
	fs, _ := a.newFlags("notice")
	notify := fs.Bool("notify", false, "show a desktop notification, only if this account is monitored")
	if _, err := parse(fs, args); err != nil {
		return 2
	}
	if !*notify {
		a.println(ChildNotice)
		return 0
	}
	resp, err := controlClient(a.Socket).Get("http://control/self")
	if err != nil {
		return 0 // the service is not running: nothing is being recorded
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return 0 // this account is not monitored
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.Exec(ctx, "notify-send", "--app-name=Parent Tracker", "Parent Tracker is recording", ChildNotice); err != nil {
		a.println(ChildNotice)
	}
	return 0
}

func commandNames() []string {
	var n []string
	for _, c := range commands() {
		n = append(n, c.name)
	}
	return n
}

func (a *App) cmdCompletion(args []string) int {
	if len(args) != 1 {
		return a.errorf("usage: parent-tracker completion bash|zsh|fish")
	}
	cmds := strings.Join(commandNames(), " ")
	switch args[0] {
	case "bash":
		a.printf(`_parent_tracker() {
  local cur="${COMP_WORDS[COMP_CWORD]}" prev="${COMP_WORDS[COMP_CWORD-1]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "%s" -- "$cur")); return
  fi
  case "${COMP_WORDS[1]}" in
    users) [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=($(compgen -W "list add remove" -- "$cur")) ;;
    config) [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=($(compgen -W "list get set" -- "$cur")) ;;
    completion) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")) ;;
    report) COMPREPLY=($(compgen -W "--user --from --to --format --by --date" -- "$cur")) ;;
  esac
}
complete -F _parent_tracker parent-tracker
`, cmds)
	case "zsh":
		a.printf(`#compdef parent-tracker
_parent_tracker() {
  local -a cmds
  cmds=(%s)
  if (( CURRENT == 2 )); then _describe 'command' cmds; return; fi
  case $words[2] in
    users) _values 'action' list add remove ;;
    config) _values 'action' list get set ;;
    completion) _values 'shell' bash zsh fish ;;
  esac
}
_parent_tracker "$@"
`, cmds)
	case "fish":
		a.println("complete -c parent-tracker -f")
		for _, c := range commands() {
			a.printf("complete -c parent-tracker -n '__fish_use_subcommand' -a %s -d %q\n", c.name, c.summary)
		}
		a.println("complete -c parent-tracker -n '__fish_seen_subcommand_from users' -a 'list add remove'")
		a.println("complete -c parent-tracker -n '__fish_seen_subcommand_from config' -a 'list get set'")
		a.println("complete -c parent-tracker -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'")
	default:
		return a.errorf("unknown shell %q (use bash, zsh or fish)", args[0])
	}
	return 0
}
