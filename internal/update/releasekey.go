package update

// ReleasePublicKeyPEM is the Ed25519 public key (SPKI PEM) that signs ship
// release checksums. The matching private key lives only in the
// SHIP_RELEASE_SIGNING_KEY GitHub Actions secret. scripts/release-keygen.sh
// generates the pair and rewrites this value. While it is empty, builds cannot
// verify signatures, so cannot install updates.
const ReleasePublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA7TfZswn+KF6dkQaHNgHC88dH1Yh7q/QoxgBmn0Tyv2I=
-----END PUBLIC KEY-----
`
