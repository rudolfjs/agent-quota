# Contributing

This repository uses automated checks, `changie`-managed release notes, and PR-driven GitHub releases (see [Release flow](#release-flow)).

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
go install github.com/miniscruff/changie@v1.25.2
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
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

Dependabot PRs (authored by `dependabot[bot]`) are exempt: dependency-bump PRs change `go.mod`/`go.sum` but can't generate a fragment, so CI skips the requirement for them.

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

Releases are cut from the GitHub web UI; no local tagging is needed.

1. **Prepare** — Actions → **Release — Prepare PR** → *Run workflow*, and enter the version (`X.Y.Z`, no leading `v`). The workflow (`.github/workflows/release-prepare.yml`) checks that the version is newer than the latest `.changes/<version>.md` and that unreleased fragments exist. It then batches them into `.changes/<version>.md`, rebuilds `CHANGELOG.md`, and opens a `release/v<version>` PR.
2. **Review and merge** — CI and **Release — PR guard** (`release-pr-guard.yml`) run on the PR. The guard checks that the version is still newer than `main`. Review the batched notes, then merge.
3. **Finalize** — merging runs **Release — Finalize on merge** (`release-finalize.yml`) on the merge commit. It runs the tests, builds the linux/amd64 archive with version injection, pushes an annotated `v<version>` tag, and publishes the GitHub Release with the changie notes, archive, checksums, and `install.sh`.

Finalize never pushes to `main` and is safe to re-run. If the tag already exists on the merge commit, it only creates the missing release. If the tag points anywhere else, it fails.

## CI summary

CI runs on PRs and push-to-main (`.github/workflows/ci.yml`). Three parallel jobs:

- **go-checks** — gofmt, go vet, golangci-lint, test, build, install script syntax
- **lefthook** — runs `pre-commit` and `pre-push` hooks in CI
- **changie** — PRs touching product code require a changie fragment in `.changes/unreleased/` (Dependabot PRs are exempt)
