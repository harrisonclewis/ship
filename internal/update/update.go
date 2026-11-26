// Package update checks for, verifies and installs ship releases.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Repo is the GitHub repository releases are published from.
	Repo = "harrisonclewis/ship"

	// ChecksumsAsset and SignatureAsset are the release metadata files.
	ChecksumsAsset = "SHA256SUMS"
	SignatureAsset = "SHA256SUMS.sig"

	// UpdateCheckInterval is how often the startup check contacts GitHub.
	UpdateCheckInterval = 24 * time.Hour

	defaultAPIURL         = "https://api.github.com/repos/" + Repo + "/releases/latest"
	defaultDownloadPrefix = "https://github.com/" + Repo + "/releases/download/"
	userAgent             = "ship-updater"
	maxMetadataBytes      = 64 << 10
	maxBinaryBytes        = 256 << 20
)

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

type parsedVersion [3]int

func (v parsedVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func parseVersion(raw string) (parsedVersion, error) {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return parsedVersion{}, fmt.Errorf("%q is not a version like 1.2.3", raw)
	}
	var v parsedVersion
	for i := range v {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return parsedVersion{}, fmt.Errorf("%q is not a version like 1.2.3", raw)
		}
		v[i] = n
	}
	return v, nil
}

// compareVersions returns a negative number when a < b, zero when they are
// equal, and a positive number when a > b.
func compareVersions(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range left {
		if left[i] != right[i] {
			return left[i] - right[i], nil
		}
	}
	return 0, nil
}

// AssetName is the release asset built for a GOOS/GOARCH pair, or an empty
// string when releases do not cover it.
func AssetName(goos, goarch string) string {
	switch goos {
	case "linux", "darwin":
		switch goarch {
		case "amd64":
			return "ship-" + goos + "-x64"
		case "arm64":
			return "ship-" + goos + "-arm64"
		}
	case "windows":
		switch goarch {
		case "amd64":
			return "ship-windows-x64.exe"
		case "arm64":
			return "ship-windows-arm64.exe"
		}
	}
	return ""
}

// ReleaseAsset is one file attached to a GitHub release.
type ReleaseAsset struct {
	Name string
	URL  string
	Size int64
}

// Release is the latest published ship release.
type Release struct {
	// Version is without the leading "v", e.g. "0.2.0".
	Version string
	Tag     string
	HTMLURL string
	Assets  []ReleaseAsset
}

// Check is the result of comparing the running version with the latest release.
type Check struct {
	CurrentVersion string
	Latest         Release
	Available      bool
	// AssetName is the release asset for this platform, or empty when none is
	// published.
	AssetName string
}

// HTTPClient is the part of http.Client used by Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Client talks to GitHub and the local update state. The zero value is usable
// through NewClient; tests override the fields to point at a fake server.
type Client struct {
	HTTP           HTTPClient
	APIURL         string
	DownloadPrefix string
	GOOS           string
	GOARCH         string
	PublicKeyPEM   string
	StateDir       string
	Getenv         func(string) string
}

// NewClient returns a client for this build's platform.
func NewClient() *Client {
	return &Client{
		HTTP:           &http.Client{},
		APIURL:         defaultAPIURL,
		DownloadPrefix: defaultDownloadPrefix,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
	}
}

func (c *Client) httpClient() HTTPClient {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) apiURL() string {
	if c.APIURL != "" {
		return c.APIURL
	}
	return defaultAPIURL
}

func (c *Client) downloadPrefix() string {
	if c.DownloadPrefix != "" {
		return c.DownloadPrefix
	}
	return defaultDownloadPrefix
}

func (c *Client) goos() string {
	if c.GOOS != "" {
		return c.GOOS
	}
	return runtime.GOOS
}

func (c *Client) goarch() string {
	if c.GOARCH != "" {
		return c.GOARCH
	}
	return runtime.GOARCH
}

func (c *Client) getenv(key string) string {
	if c.Getenv != nil {
		return c.Getenv(key)
	}
	return os.Getenv(key)
}

// Check compares current with the latest GitHub release.
func (c *Client) Check(ctx context.Context, current string) (*Check, error) {
	latest, err := c.fetchLatestRelease(ctx)
	if err != nil {
		return nil, err
	}
	assetName := AssetName(c.goos(), c.goarch())
	published := ""
	if assetName != "" {
		for _, asset := range latest.Assets {
			if asset.Name == assetName {
				published = assetName
				break
			}
		}
	}
	cmp, err := compareVersions(latest.Version, current)
	if err != nil {
		return nil, err
	}
	return &Check{
		CurrentVersion: current,
		Latest:         *latest,
		Available:      cmp > 0,
		AssetName:      published,
	}, nil
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func (c *Client) fetchLatestRelease(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub to check for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("no ship release has been published yet")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned HTTP %d while checking for updates", resp.StatusCode)
	}
	if resp.ContentLength > maxMetadataBytes {
		return nil, errors.New("the release information is larger than expected")
	}
	body, err := readLimited(resp.Body, maxMetadataBytes, "release information")
	if err != nil {
		return nil, err
	}

	var parsed releaseResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("GitHub returned an unreadable release response: %w", err)
	}
	v, err := parseVersion(parsed.TagName)
	if err != nil {
		return nil, fmt.Errorf("the latest release tag %q is not a version", parsed.TagName)
	}

	prefix := c.downloadPrefix() + parsed.TagName + "/"
	assets := make([]ReleaseAsset, 0, len(parsed.Assets))
	for _, asset := range parsed.Assets {
		if asset.Name == "" || asset.BrowserDownloadURL == "" {
			continue
		}
		// Only ever download from this repository's release files.
		if !strings.HasPrefix(asset.BrowserDownloadURL, prefix) {
			continue
		}
		assets = append(assets, ReleaseAsset{
			Name: asset.Name,
			URL:  asset.BrowserDownloadURL,
			Size: asset.Size,
		})
	}
	return &Release{
		Version: v.String(),
		Tag:     parsed.TagName,
		HTMLURL: parsed.HTMLURL,
		Assets:  assets,
	}, nil
}

func readLimited(r io.Reader, max int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("could not read the %s: %w", what, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the %s is larger than expected", what)
	}
	return data, nil
}
