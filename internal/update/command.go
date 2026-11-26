package update

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/harrisonclewis/ship/internal/version"
)

const updateTimeout = 10 * time.Minute

// Run implements `ship update [--check]`.
func Run(args []string, out io.Writer) error {
	checkOnly := false
	for _, arg := range args {
		if arg != "--check" {
			return fmt.Errorf("unknown update option %q", arg)
		}
		checkOnly = true
	}

	current := version.Version
	if _, err := parseVersion(current); err != nil {
		return fmt.Errorf("ship update only works for release builds (this is %s); install a release instead", current)
	}

	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	check, err := client.Check(ctx, current)
	if err != nil {
		return err
	}
	if !check.Available {
		fmt.Fprintf(out, "ship %s is up to date.\n", current)
		return nil
	}
	if check.AssetName == "" {
		return fmt.Errorf("ship %s is available, but this platform has no release asset", check.Latest.Version)
	}
	if checkOnly {
		fmt.Fprintf(out, "ship %s is available. Run `ship update` to install it.\n", check.Latest.Version)
		return nil
	}

	fmt.Fprintf(out, "Updating ship %s → %s…\n", current, check.Latest.Version)
	binary, err := client.DownloadVerified(ctx, &check.Latest, check.AssetName)
	if err != nil {
		return err
	}
	if _, err := Install(binary, "", runtime.GOOS); err != nil {
		return err
	}
	_ = client.writeState(updateState{
		CheckedAt:     time.Now().UTC().Format(time.RFC3339),
		LatestVersion: check.Latest.Version,
	})
	fmt.Fprintf(out, "Updated to %s. Restart ship to use it.\n", check.Latest.Version)
	return nil
}
