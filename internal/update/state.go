package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stateFileName = "update-check.json"

type updateState struct {
	CheckedAt     string `json:"checked_at,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
}

// Notice reports a newer release than the running build.
type Notice struct {
	CurrentVersion string
	LatestVersion  string
}

func (c *Client) stateDir() (string, error) {
	if c.StateDir != "" {
		return c.StateDir, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "ship"), nil
}

func (c *Client) statePath() (string, error) {
	dir, err := c.stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFileName), nil
}

func (c *Client) readState() updateState {
	path, err := c.statePath()
	if err != nil {
		return updateState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return updateState{}
	}
	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return updateState{}
	}
	if state.LatestVersion != "" {
		if _, err := parseVersion(state.LatestVersion); err != nil {
			state.LatestVersion = ""
		}
	}
	return state
}

func (c *Client) writeState(state updateState) error {
	path, err := c.statePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(dir, 0o700)

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".update-check-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (c *Client) checkDisabled() bool {
	flag := strings.TrimSpace(c.getenv("SHIP_NO_UPDATE_CHECK"))
	return flag != "" && flag != "0"
}

func (c *Client) stateFresh(state updateState, now time.Time) bool {
	checkedAt, err := time.Parse(time.RFC3339, state.CheckedAt)
	if err != nil {
		return false
	}
	age := now.Sub(checkedAt)
	return age >= 0 && age < UpdateCheckInterval
}

func noticeFor(current, latest string) *Notice {
	if latest == "" {
		return nil
	}
	cmp, err := compareVersions(latest, current)
	if err != nil || cmp <= 0 {
		return nil
	}
	return &Notice{CurrentVersion: current, LatestVersion: latest}
}

// BackgroundCheck returns a notice when a newer release is known and refreshes
// the cached release at most once a day. It never returns an error: offline
// runs fall back to the cached version.
func (c *Client) BackgroundCheck(ctx context.Context, current string, now time.Time) *Notice {
	if c.checkDisabled() {
		return nil
	}
	if _, err := parseVersion(current); err != nil {
		return nil
	}
	state := c.readState()
	if c.stateFresh(state, now) {
		return noticeFor(current, state.LatestVersion)
	}

	check, err := c.Check(ctx, current)
	if err != nil {
		return noticeFor(current, state.LatestVersion)
	}
	_ = c.writeState(updateState{
		CheckedAt:     now.UTC().Format(time.RFC3339),
		LatestVersion: check.Latest.Version,
	})
	if check.AssetName == "" {
		return nil
	}
	return noticeFor(current, check.Latest.Version)
}
