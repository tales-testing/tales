# Release

Tales releases are cut from semver-style git tags. Pushing a tag matching
`v*` triggers the [release workflow](../.github/workflows/release.yml),
which runs the test suite and then uses [GoReleaser](https://goreleaser.com)
to cross-compile the `tales` binary, upload archives to the GitHub Release,
and write a `checksums.txt` file.

## Cutting a release

1. Make sure `master` is green (lint + unit + e2e all pass in
   [the dev CI workflow](../.github/workflows/go.yml)).
2. Tag and push from `master`:

   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

   Tag format: `vMAJOR.MINOR.PATCH`. Pre-release suffixes such as
   `v0.1.0-rc.1` or `v0.1.0-beta.2` are accepted and are flagged as
   GitHub pre-releases automatically (`release.prerelease: auto` in
   [.goreleaser.yml](../.goreleaser.yml)).

3. Watch the `Release` workflow in the GitHub Actions tab. On success
   the GitHub Release is created with the artifacts listed below.

## Supported platforms

| OS     | Architectures   |
| ------ | --------------- |
| Linux  | `amd64`, `arm64` |
| macOS  | `amd64`, `arm64` |

No Windows build is produced in V1.

## Artifacts

For a tag `v0.1.0` the workflow publishes:

- `tales_0.1.0_linux_x86_64.tar.gz`
- `tales_0.1.0_linux_arm64.tar.gz`
- `tales_0.1.0_darwin_x86_64.tar.gz`
- `tales_0.1.0_darwin_arm64.tar.gz`
- `checksums.txt` (SHA-256, one line per archive)

Each archive contains the `tales` binary, `LICENSE.md`, and `README.md`.

The leading `v` from the git tag is stripped from artifact names, so a
tag `v0.1.0` produces `tales_0.1.0_*.tar.gz`.

## Verifying a release

After download, validate the checksum and confirm the embedded build
metadata:

```bash
# Verify checksum
shasum -a 256 -c checksums.txt --ignore-missing

# Extract and inspect
tar -xzf tales_0.1.0_linux_x86_64.tar.gz
./tales --version
```

`tales --version` should report the released tag and full commit SHA:

```
tales version: 0.1.0 (build: 2026-05-17T10:00:00Z)
commit: a1b2c3d4e5f6...
Go runtime version: go1.26.x
Platform: linux/amd64
```

If `version` is `dev` or `commit` is `none`, the binary was not built
through the release pipeline.

## Local snapshot builds

To smoke-test a release without pushing a tag, use the wrapper targets
in the [Makefile](../Makefile):

```bash
make release-check       # validates .goreleaser.yml
make release-snapshot    # full snapshot: archives + checksums in dist/
make release-build       # binaries only, no archives
```

These require `goreleaser` on `$PATH`
(see [installation instructions](https://goreleaser.com/install/)).

The snapshot output lives under `dist/` (gitignored). The version
template is `<next-patch>-next`, so a working tree at `v0.1.0` produces
`tales_0.1.1-next_*.tar.gz` archives.

## V1 limitations

The following are intentionally out of scope for V1 and will be
considered later:

- **Windows builds** — not provided. The DSL and runtime are
  cross-platform, but no `windows` GOOS targets are configured.
- **Docker images** — no container build is published.
- **Homebrew tap / scoop / nfpm packaging** — install by downloading
  the archive or with `go install github.com/hyperxlab/tales/cmd/tales@latest`.
- **macOS code signing and notarization** — released binaries are not
  signed. On a fresh macOS install you may need to run
  `xattr -d com.apple.quarantine tales` after extraction.
- **iOS / XCUITest e2e** — the embedded Apple driver source ships in
  every macOS binary, but the release workflow runs on Ubuntu and does
  **not** execute `make e2e-ios` / `make e2e-ios-failure`. iOS e2e
  must be validated manually on a macOS+Xcode host before tagging.
- **Artifact signatures (cosign / SBOM)** — not generated.
