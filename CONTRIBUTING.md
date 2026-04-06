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

Releases are driven by pushing a semantic version tag like `v0.1.0`.

### 1. Prepare the release notes

Batch unreleased fragments into a versioned release file:

```bash
changie batch 0.1.0
```

> Do not include the `v` prefix in `changie batch`.
> `changie batch v0.1.0` creates the wrong filename.

Merge the versioned release notes into `CHANGELOG.md`:

```bash
changie merge
```

### 2. Commit and merge the release changes

Commit the generated files, then make sure that commit is merged to `main`.

The release workflow expects the tagged commit to already be on `main`.

### 3. Tag the release commit on `main`

Create and push the release tag:

```bash
git tag v0.1.0
git push origin main --tags
```

## What the GitHub release workflow does

When a `v*` tag is pushed, GitHub Actions will:

1. Verify the tag points to a commit on `main`
2. Verify `.changes/<version>.md` exists
3. Run the release verification checks
4. Build the release artifacts
5. Generate checksums
6. Publish the GitHub Release using the changie release notes

## CI summary

The repository CI runs:

- Go formatting checks
- `go vet`
- `golangci-lint`
- tests with `-race`
- build verification
- Lefthook validation
- changie fragment and release note validation
