package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

type fakeRelease struct {
	version       string
	signedVersion string
	tag           string
	binary        []byte
	checksums     []byte
	signature     []byte
	assetName     string
	assetURLBase  string
	privateKey    ed25519.PrivateKey
	publicPEM     string

	mu         sync.Mutex
	apiHits    int
	binaryHits int
}

func newFakeRelease(t *testing.T, version string, binary []byte) *fakeRelease {
	t.Helper()
	private, publicPEM := testSigningKey(t)
	f := &fakeRelease{
		version:    version,
		tag:        "v" + version,
		binary:     binary,
		assetName:  AssetName("linux", "amd64"),
		privateKey: private,
		publicPEM:  publicPEM,
	}
	f.checksums = []byte(sha256Hex(binary) + "  " + f.assetName + "\n")
	return f
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func (f *fakeRelease) start(t *testing.T) (*Client, *httptest.Server) {
	t.Helper()
	if f.signature == nil {
		signedVersion := f.signedVersion
		if signedVersion == "" {
			signedVersion = f.version
		}
		f.signature = ed25519.Sign(f.privateKey, signedMessage(signedVersion, f.checksums))
	}

	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.apiHits++
		f.mu.Unlock()
		base := f.assetURLBase
		if base == "" {
			base = server.URL + "/download/" + f.tag + "/"
		}
		assets := []map[string]any{
			{"name": ChecksumsAsset, "browser_download_url": base + ChecksumsAsset, "size": len(f.checksums)},
			{"name": SignatureAsset, "browser_download_url": base + SignatureAsset, "size": len(f.signature)},
			{"name": f.assetName, "browser_download_url": base + f.assetName, "size": len(f.binary)},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": f.tag,
			"html_url": "https://github.com/" + Repo + "/releases/tag/" + f.tag,
			"assets":   assets,
		})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		switch name := path.Base(r.URL.Path); name {
		case ChecksumsAsset:
			_, _ = w.Write(f.checksums)
		case SignatureAsset:
			_, _ = w.Write(f.signature)
		case f.assetName:
			f.mu.Lock()
			f.binaryHits++
			f.mu.Unlock()
			_, _ = w.Write(f.binary)
		default:
			http.NotFound(w, r)
		}
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := NewClient()
	client.HTTP = server.Client()
	client.APIURL = server.URL + "/releases/latest"
	client.DownloadPrefix = server.URL + "/download/"
	client.GOOS = "linux"
	client.GOARCH = "amd64"
	client.PublicKeyPEM = f.publicPEM
	return client, server
}

func (f *fakeRelease) hits() (api, binary int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apiHits, f.binaryHits
}

func TestCheckForUpdate(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("new binary"))
	client, _ := f.start(t)

	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !check.Available || check.Latest.Version != "0.2.0" || check.AssetName != "ship-linux-x64" {
		t.Fatalf("check = %+v", check)
	}

	current, err := client.Check(context.Background(), "0.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if current.Available {
		t.Fatalf("current release reported as available: %+v", current)
	}
}

func TestCheckIgnoresAssetsOutsideTheRelease(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("new"))
	f.assetURLBase = "https://evil.example/download/v0.2.0/"
	client, _ := f.start(t)

	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Latest.Assets) != 0 || check.AssetName != "" {
		t.Fatalf("assets = %+v, name = %q", check.Latest.Assets, check.AssetName)
	}
}

func TestCheckNoRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewClient()
	client.HTTP = server.Client()
	client.APIURL = server.URL
	if _, err := client.Check(context.Background(), "0.1.0"); err == nil || !strings.Contains(err.Error(), "no ship release") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadVerified(t *testing.T) {
	binary := []byte("new binary")
	f := newFakeRelease(t, "0.2.0", binary)
	client, _ := f.start(t)
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("binary = %q", got)
	}
}

func TestDownloadAndInstall(t *testing.T) {
	binary := []byte("new binary")
	f := newFakeRelease(t, "0.2.0", binary)
	client, _ := f.start(t)
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "ship")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(downloaded, target, runtime.GOOS); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != string(binary) {
		t.Fatalf("installed content = %q, err %v", data, err)
	}
}

func TestDownloadRejectsOldReleaseRelabelledAsNew(t *testing.T) {
	f := newFakeRelease(t, "0.3.0", []byte("old"))
	f.signedVersion = "0.2.0"
	client, _ := f.start(t)
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err = %v", err)
	}
	if _, binaryHits := f.hits(); binaryHits != 0 {
		t.Fatalf("binary downloaded before the signature was verified (%d hits)", binaryHits)
	}
}

func TestDownloadRejectsWrongKey(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	_, otherPEM := testSigningKey(t)
	client.PublicKeyPEM = otherPEM
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("tampered"))
	f.checksums = []byte(sha256Hex([]byte("expected")) + "  " + f.assetName + "\n")
	client, _ := f.start(t)
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadRefusesWithoutKey(t *testing.T) {
	f := newFakeRelease(t, "0.2.0", []byte("x"))
	client, _ := f.start(t)
	client.PublicKeyPEM = " "
	check, err := client.Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.DownloadVerified(context.Background(), &check.Latest, check.AssetName); err == nil || !strings.Contains(err.Error(), "signing key") {
		t.Fatalf("err = %v", err)
	}
}
