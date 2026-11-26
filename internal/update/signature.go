package update

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const signaturePrefix = "ship-release v"

var checksumLinePattern = regexp.MustCompile(`^([0-9a-f]{64}) [ *](.+)$`)

// signedMessage is the exact byte sequence the release workflow signs. Binding
// the version into the message stops an older, validly signed release being
// republished as newer.
func signedMessage(version string, checksums []byte) []byte {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	message := make([]byte, 0, len(signaturePrefix)+len(v)+1+len(checksums))
	message = append(message, signaturePrefix...)
	message = append(message, v...)
	message = append(message, '\n')
	message = append(message, checksums...)
	return message
}

// verifySignature checks the Ed25519 signature over the version-bound checksum
// list. It returns an error when the build has no usable key, and false when
// the signature does not match.
func verifySignature(version string, checksums, signature []byte, publicKeyPEM string) (bool, error) {
	if strings.TrimSpace(publicKeyPEM) == "" {
		return false, errors.New("this build has no release signing key, so it cannot verify updates")
	}
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil || block.Type != "PUBLIC KEY" {
		return false, errors.New("the embedded release signing key is not a public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("parse release signing key: %w", err)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return false, errors.New("the release signing key is not an Ed25519 key")
	}
	if len(signature) != ed25519.SignatureSize {
		return false, nil
	}
	return ed25519.Verify(publicKey, signedMessage(version, checksums), signature), nil
}

// expectedChecksum finds the SHA-256 for one file in sha256sum output.
func expectedChecksum(checksums, assetName string) (string, error) {
	var matches []string
	for _, line := range strings.Split(strings.ReplaceAll(checksums, "\r\n", "\n"), "\n") {
		match := checksumLinePattern.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if match != nil && match[2] == assetName {
			matches = append(matches, match[1])
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("the release checksums do not list %s exactly once", assetName)
	}
	return matches[0], nil
}
