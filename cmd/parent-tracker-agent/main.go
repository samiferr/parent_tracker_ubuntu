// Command parent-tracker-agent runs inside a monitored user's graphical session
// and reports the application in front to parent-trackerd. It collects nothing
// until the daemon confirms that this user is monitored, and it exits having
// collected nothing otherwise.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/samiferr/parent_tracker_ubuntu/internal/agent"
	"github.com/samiferr/parent_tracker_ubuntu/internal/appmap"
	"github.com/samiferr/parent_tracker_ubuntu/internal/model"
	"github.com/samiferr/parent_tracker_ubuntu/internal/version"
)

func main() {
	socket := flag.String("socket", "/run/parent-tracker/agent.sock", "daemon agent socket")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("parent-tracker-agent", version.Version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey && os.Getenv("JOURNAL_STREAM") != "" {
			return slog.Attr{}
		}
		return a
	}}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// One agent per user. It is started by the systemd user unit where the desktop
	// reaches graphical-session.target (GNOME) and by an XDG autostart entry
	// everywhere (Xfce never reaches that target); whichever comes second leaves.
	release, got, err := agent.Lock(agent.LockFile(os.Getenv("XDG_RUNTIME_DIR"), os.Getuid()))
	switch {
	case err != nil:
		log.Warn("cannot take the single-instance lock; starting anyway", "err", err)
	case !got:
		log.Info("another agent is already running for this user; exiting")
		return // exit 0: systemd does not restart a clean exit
	default:
		defer release()
	}

	apps := appmap.New()
	err = agent.Run(ctx, agent.Options{
		Socket: *socket, Log: log,
		SessionHint: os.Getenv("XDG_SESSION_ID"), SessionType: os.Getenv("XDG_SESSION_TYPE"),
		NewProvider: func(p model.Provider) agent.Provider {
			switch p {
			case model.ProviderX11:
				return agent.NewX11(apps)
			case model.ProviderGnomeWayland:
				return agent.NewGnomeWayland()
			}
			return nil
		},
	})
	var nm agent.ErrNotMonitored
	if errors.As(err, &nm) {
		log.Info("this account is not monitored; nothing is collected", "reason", nm.Reason)
		return // exit 0: systemd does not restart a clean exit
	}
	if err != nil {
		log.Error("agent stopped", "err", err)
		os.Exit(1)
	}
}
