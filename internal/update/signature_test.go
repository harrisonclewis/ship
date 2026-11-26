package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func testSigningKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return private, string(block)
}

func TestVerifySignature(t *testing.T) {
	private, publicPEM := testSigningKey(t)
	checksums := []byte("abc  ship-linux-x64\n")
	signature := ed25519.Sign(private, signedMessage("1.0.0", checksums))

	valid, err := verifySignature("1.0.0", checksums, signature, publicPEM)
	if err != nil || !valid {
		t.Fatalf("valid signature rejected: valid=%v err=%v", valid, err)
	}

	_, otherPEM := testSigningKey(t)
	for name, test := range map[string]struct {
		version   string
		checksums []byte
		signature []byte
		key       string
	}{
		"wrong version":   {"1.0.1", checksums, signature, publicPEM},
		"wrong checksums": {"1.0.0", []byte("abd  ship-linux-x64\n"), signature, publicPEM},
		"wrong key":       {"1.0.0", checksums, signature, otherPEM},
		"short signature": {"1.0.0", checksums, signature[:10], publicPEM},
	} {
		valid, err := verifySignature(test.version, test.checksums, test.signature, test.key)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", name, err)
		}
		if valid {
			t.Errorf("%s: signature accepted", name)
		}
	}
}

func TestVerifySignatureRequiresKey(t *testing.T) {
	if _, err := verifySignature("1.0.0", nil, make([]byte, ed25519.SignatureSize), ""); err == nil || !strings.Contains(err.Error(), "no release signing key") {
		t.Fatalf("err = %v", err)
	}
	if _, err := verifySignature("1.0.0", nil, make([]byte, ed25519.SignatureSize), "not a pem"); err == nil || !strings.Contains(err.Error(), "not a public key") {
		t.Fatalf("err = %v", err)
	}
}

func TestExpectedChecksum(t *testing.T) {
	hash := strings.Repeat("a", 64)
	if got, err := expectedChecksum(hash+"  ship-linux-x64\n", "ship-linux-x64"); err != nil || got != hash {
		t.Fatalf("got %q, err %v", got, err)
	}
	if got, err := expectedChecksum(hash+" *ship-linux-x64\n", "ship-linux-x64"); err != nil || got != hash {
		t.Fatalf("star form: got %q, err %v", got, err)
	}
	if got, err := expectedChecksum(hash+"  ship-linux-x64\r\n", "ship-linux-x64"); err != nil || got != hash {
		t.Fatalf("CRLF form: got %q, err %v", got, err)
	}
	for _, sums := range []string{
		hash + "  other\n",
		hash + "  ship-linux-x64\n" + hash + "  ship-linux-x64\n",
	} {
		if _, err := expectedChecksum(sums, "ship-linux-x64"); err == nil {
			t.Errorf("expectedChecksum(%q) succeeded, want an error", sums)
		}
	}
}
