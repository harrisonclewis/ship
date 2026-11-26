package update

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"
)

const startupTimeout = 5 * time.Second

// Startup performs the cheap Windows cleanup and runs the daily update notice.
// It prints only from the cached result, so a short command is never delayed by
// the network; a stale cache is refreshed in the background.
func Startup(currentVersion string, args []string, tty bool, errOut io.Writer) {
	if runtime.GOOS == "windows" {
		CleanupPreviousBinary("")
	}
	startup(NewClient(), currentVersion, args, tty, errOut)
}

func startup(c *Client, currentVersion string, args []string, tty bool, errOut io.Writer) {
	if !tty || skipsStartupCheck(args) || c.checkDisabled() {
		return
	}
	if _, err := parseVersion(currentVersion); err != nil {
		return
	}
	state := c.readState()
	if notice := noticeFor(currentVersion, state.LatestVersion); notice != nil && errOut != nil {
		fmt.Fprintf(errOut, "ship %s is available; run `ship update` to install it.\n", notice.LatestVersion)
	}
	if c.stateFresh(state, time.Now()) {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()
		_ = c.BackgroundCheck(ctx, currentVersion, time.Now())
	}()
}

func skipsStartupCheck(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "update", "version":
		return true
	default:
		return false
	}
}
