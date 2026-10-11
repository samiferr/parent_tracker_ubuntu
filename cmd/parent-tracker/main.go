// Command parent-tracker is the administration command for Parent Tracker.
package main

import (
	"os"

	"github.com/samiferr/parent_tracker_ubuntu/internal/cli"
	"github.com/samiferr/parent_tracker_ubuntu/internal/version"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.NewApp(version.Version)))
}
