.PHONY: build test lint fmt changie-check release-check hooks-install ci local-install

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
	go build -o /tmp/$(BINARY) $(CMD)

hooks-install:
	lefthook install

ci: release-check

local-install: build
	@if [ "$$(uname -s)" != "Linux" ]; then \
		echo "local-install is supported on Linux x86_64 only; build manually on other platforms if you want to experiment" >&2; \
		exit 1; \
	fi
	@if [ "$$(uname -m)" != "x86_64" ] && [ "$$(uname -m)" != "amd64" ]; then \
		echo "local-install is supported on Linux x86_64 only; build manually on other platforms if you want to experiment" >&2; \
		exit 1; \
	fi
	install -m 0755 $(BINARY) $$HOME/.local/bin/$(BINARY)
