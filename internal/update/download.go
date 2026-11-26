package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// DownloadVerified downloads assetName from release and returns its bytes only
// after the signed checksum list and the binary's SHA-256 both check out.
func (c *Client) DownloadVerified(ctx context.Context, release *Release, assetName string) ([]byte, error) {
	find := func(name string) (*ReleaseAsset, error) {
		for i := range release.Assets {
			if release.Assets[i].Name == name {
				return &release.Assets[i], nil
			}
		}
		return nil, fmt.Errorf("release %s has no %s file", release.Tag, name)
	}

	checksumsAsset, err := find(ChecksumsAsset)
	if err != nil {
		return nil, err
	}
	signatureAsset, err := find(SignatureAsset)
	if err != nil {
		return nil, err
	}
	binaryAsset, err := find(assetName)
	if err != nil {
		return nil, err
	}

	checksums, err := c.download(ctx, checksumsAsset, maxMetadataBytes, "release checksums")
	if err != nil {
		return nil, err
	}
	signature, err := c.download(ctx, signatureAsset, maxMetadataBytes, "release signature")
	if err != nil {
		return nil, err
	}

	publicKeyPEM := c.PublicKeyPEM
	if publicKeyPEM == "" {
		publicKeyPEM = ReleasePublicKeyPEM
	}
	valid, err := verifySignature(release.Version, checksums, signature, publicKeyPEM)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, fmt.Errorf("the signature on release %s is not valid; not updating", release.Tag)
	}
	expected, err := expectedChecksum(string(checksums), assetName)
	if err != nil {
		return nil, err
	}

	binary, err := c.download(ctx, binaryAsset, maxBinaryBytes, "update")
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(binary)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return nil, fmt.Errorf("the downloaded %s does not match its checksum; not updating", assetName)
	}
	return binary, nil
}

func (c *Client) download(ctx context.Context, asset *ReleaseAsset, max int64, what string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", what, err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not download the %s: %w", what, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned HTTP %d for the %s", resp.StatusCode, what)
	}
	if resp.ContentLength > max {
		return nil, fmt.Errorf("the %s is larger than expected", what)
	}
	return readLimited(resp.Body, max, what)
}
