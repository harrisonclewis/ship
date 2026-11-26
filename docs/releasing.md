# Releasing

## One-time setup

Run `scripts/release-keygen.sh`. It writes the public key into `internal/update/releasekey.go` and prints the `gh secret set` command that stores the private key as the `SHIP_RELEASE_SIGNING_KEY` secret of the `release` environment. Commit the public key. Back up the private key: installed copies only accept updates it signed.

## Each release

1. Run `make check`, commit, push `main` and wait for CI (Linux, macOS and Windows).
2. Push an annotated tag whose message is the release title:

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z: <what changed>"
   git push origin vX.Y.Z
   ```

3. The release workflow builds all six binaries, smoke-tests them on Linux, macOS and Windows, signs `SHA256SUMS` and uploads everything to a **draft** release.
4. Download the draft and check it: eight files (six binaries, `SHA256SUMS`, `SHA256SUMS.sig`), and `sha256sum -c SHA256SUMS` passes.
5. Publish it with the written notes:

   ```sh
   gh release edit vX.Y.Z --title "vX.Y.Z: …" --notes-file notes.md --draft=false --latest
   ```

Never move or delete the tag of a published release, and never replace its files. Fix forward with the next patch release.

To build all six binaries locally, run `make dist VERSION=X.Y.Z`; they land in `dist/`.
