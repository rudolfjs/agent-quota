# Contributing

This repository uses automated checks, `changie`-managed release notes, and tag-driven GitHub releases.

## Prerequisites

- Go 1.25+ ([install from go.dev/dl](https://go.dev/dl/))

## Initial setup

After cloning, run:

```bash
make install-deps
```

This will:

1. Verify your Go version meets the minimum (1.25.0)
2. Install development tools: lefthook, changie, golangci-lint
3. Warn if `$(go env GOPATH)/bin` is not on your PATH
4. Download Go module dependencies
5. Set up Git hooks via lefthook

### Manual tool install (if needed)

If you prefer to install tools individually:

```bash
go install github.com/evilmartians/lefthook/v2@latest
go install github.com/miniscruff/changie@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
make hooks-install
```

## Local development

Run the standard local checks before opening a PR:

```bash
make release-check
make build
```

That covers:

- `gofmt`
- `go vet`
- `golangci-lint`
- `go test -race`
- changie validation
- installer script shell validation
- CLI build verification

## Pull request workflow

1. Create a feature branch from `main`
2. Make the change and add or update tests as needed
3. Add a `changie` fragment for product changes
4. Run `make release-check`
5. Commit with a Conventional Commit message
6. Open a PR

### Commit messages

The `commit-msg` hook enforces Conventional Commits:

- `feat: ...`
- `fix: ...`
- `docs: ...`
- `chore: ...`
- etc.

## Changelog workflow

This repo uses `changie` for unreleased fragments and versioned release notes.

### When a fragment is required

CI requires a changie fragment for non-test product changes in:

- `cmd/`
- `internal/`
- `go.mod`
- `go.sum`
- `scripts/install.sh`
- `lefthook.yml`

Changes limited to docs, tests, or other non-product files do not need a fragment.

### For normal PRs

Add one unreleased fragment per logical change:

```bash
changie new --interactive=false --kind Added --body 'New `agent-quota` feature description'
```

Use one of these kinds:

- `Added`
- `Changed`
- `Deprecated`
- `Removed`
- `Fixed`
- `Security`

## Release flow

Releases are fully automated. Just tag and push:

```bash
git tag v0.2.0
git push origin v0.2.0
```

The release pipeline (`.github/workflows/release.yml`) will:

1. Verify the tag points to a commit on `main`
2. Run tests and build verification
3. Auto-batch unreleased changie fragments into `.changes/<version>.md` and update `CHANGELOG.md`
4. Commit the changelog updates directly to `main` (via GitHub App token with ruleset bypass)
5. Move the tag to include the changelog commit
6. Build Linux x86_64, macOS Intel, and macOS Apple Silicon binaries with version injection
7. Publish the GitHub Release with changie notes, artifacts, checksums, and `install.sh`

> If `.changes/<version>.md` already exists (manual batch), the pipeline skips steps 3–5 and uses the existing notes.

## CI summary

CI runs on PRs and push-to-main (`.github/workflows/ci.yml`). Three parallel jobs:

- **go-checks** — gofmt, go vet, golangci-lint, test, build, install script syntax
- **lefthook** — runs `pre-commit` and `pre-push` hooks in CI
- **changie** — PRs touching product code require a changie fragment in `.changes/unreleased/`
