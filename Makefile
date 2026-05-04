.PHONY: build test lint fmt changie-check release-check hooks-install ci local-install install-deps

BINARY := agent-quota
CMD := ./cmd/agent-quota/

build:
	go build -o $(BINARY) $(CMD)

test:
	go test -race -count=1 ./...

lint:
	go vet ./...
	golangci-lint run ./...

fmt:
	@files=$$(find . -type f -name '*.go' -print0 | xargs -0 gofmt -l); \
	if [ -n "$$files" ]; then \
		printf '%s\n' "$$files"; \
		exit 1; \
	fi

changie-check:
	@if find .changes/unreleased -maxdepth 1 -type f ! -name '.gitkeep' | grep -q .; then \
		changie batch auto --dry-run >/dev/null; \
	elif find .changes -maxdepth 1 -type f -name '[0-9]*.md' | grep -q .; then \
		echo "release notes detected"; \
	else \
		echo "no changie fragments or release notes found" >&2; \
		exit 1; \
	fi

release-check: fmt lint test changie-check
	sh -n scripts/install.sh
	sh -n scripts/install-deps.sh
	go build -o /tmp/$(BINARY) $(CMD)

hooks-install:
	lefthook install

install-deps:
	sh scripts/install-deps.sh

ci: release-check

local-install: build
	@if [ "$$(uname -s)" != "Linux" ] && [ "$$(uname -s)" != "Darwin" ]; then \
		echo "local-install is supported on Linux x86_64, macOS Intel, and macOS Apple Silicon" >&2; \
		exit 1; \
	fi
	@if [ "$$(uname -s)" = "Linux" ] && [ "$$(uname -m)" != "x86_64" ] && [ "$$(uname -m)" != "amd64" ]; then \
		echo "local-install is supported on Linux x86_64 only for Linux hosts" >&2; \
		exit 1; \
	fi
	@if [ "$$(uname -s)" = "Darwin" ] && [ "$$(uname -m)" != "x86_64" ] && [ "$$(uname -m)" != "amd64" ] && [ "$$(uname -m)" != "arm64" ] && [ "$$(uname -m)" != "aarch64" ]; then \
		echo "local-install is supported on macOS Intel and macOS Apple Silicon" >&2; \
		exit 1; \
	fi
	install -m 0755 $(BINARY) $$HOME/.local/bin/$(BINARY)
	ln -sf $$HOME/.local/bin/$(BINARY) $$HOME/.local/bin/aq
