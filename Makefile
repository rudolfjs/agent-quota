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
	changie batch auto --dry-run >/dev/null

release-check: fmt lint test changie-check
	sh -n install.sh
	go build -o /tmp/$(BINARY) $(CMD)

hooks-install:
	lefthook install

ci: release-check

local-install: build
	install -m 0755 $(BINARY) $$HOME/.local/bin/$(BINARY)
