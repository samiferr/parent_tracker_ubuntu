// Command parent-trackerd is the Parent Tracker system service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/samiferr/parent_tracker_ubuntu/internal/config"
	"github.com/samiferr/parent_tracker_ubuntu/internal/daemon"
	"github.com/samiferr/parent_tracker_ubuntu/internal/osrel"
	"github.com/samiferr/parent_tracker_ubuntu/internal/version"
)

func main() {
	cfgPath := flag.String("config", config.DefaultPath, "path to config.toml")
	flag.StringVar(cfgPath, "c", config.DefaultPath, "path to config.toml (short for --config)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("parent-trackerd", version.Version)
		return
	}

	level := &slog.LevelVar{}
	opts := &slog.HandlerOptions{Level: level}
	if os.Getenv("JOURNAL_STREAM") != "" { // journald adds the timestamp
		opts.ReplaceAttr = func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, opts))
	slog.SetDefault(log)

	if in, err := osrel.Read("/etc/os-release"); err == nil {
		if v, msg := osrel.Judge(in); v == osrel.Unsupported {
			log.Warn("running on an unsupported OS", "detail", msg)
		}
	}

	d, err := daemon.New(daemon.Options{ConfigPath: *cfgPath, Version: version.Version, Log: log, LogLevel: level, Loc: time.Local})
	if err != nil {
		log.Error("cannot start", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := d.Reload(); err != nil {
				log.Error("reload failed", "err", err)
			}
		}
	}()
	if err := d.Run(ctx); err != nil {
		log.Error("stopped with an error", "err", err)
		os.Exit(1)
	}
}
