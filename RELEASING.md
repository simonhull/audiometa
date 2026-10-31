# Releasing

Releases are built by GoReleaser from a version tag.

1. Make sure `main` is green in CI and `CHANGELOG.md` has the release notes:
   rename the `Unreleased` section to the new version and date.
2. Tag and push:

   ```bash
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

3. The `Release` workflow (`.github/workflows/release.yml`) runs on the tag and
   publishes a GitHub release containing:
   - `audiometa_X.Y.Z_<os>_<arch>.tar.gz` (`.zip` on Windows) for
     linux/darwin/windows on amd64 and arm64, each with `LICENSE` and `README.md`
   - `checksums.txt`
   - a changelog generated from commit messages (grouped by gitmoji prefix)

The binaries are static (`CGO_ENABLED=0`, `-trimpath`) and report the tag via
`audiometa version`.

Go users can also install straight from the module proxy:

```bash
go install github.com/simonhull/audiometa/cmd/audiometa@latest
```

To test the release build locally without publishing:

```bash
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```
