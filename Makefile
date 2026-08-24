BINARY := herdr-auto-title
PKG    := ./cmd/herdr-auto-title

.DEFAULT_GOAL := help

.PHONY: help
help: ## List every target
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk -F':.*?## ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: check
check: fmt vet test ## Everything that must be green before a commit

.PHONY: fmt
fmt: ## Format the code
	@gofmt -l -w .

.PHONY: vet
vet: ## Static checks
	@go vet ./...

.PHONY: test
test: ## Tests with the race detector
	@go test -race ./...

.PHONY: test-v
test-v: ## Tests, verbose
	@go test -race -v ./...

.PHONY: build
build: ## Build the binary
	@go build -o $(BINARY) $(PKG)

.PHONY: run
run: build ## Run in the current Herdr session with DEBUG logging
	@HERDR_AUTO_TITLE_DEBUG=1 ./$(BINARY)

.PHONY: dev
dev: ## Rebuild and restart on every source change
	@./scripts/watch.sh

.PHONY: stop
stop: ## Stop forgotten plugin and watcher instances
	@pkill -f 'scripts/watch.sh' 2>/dev/null && echo "watcher stopped" || true
	@pkill -f '/$(BINARY)$$' 2>/dev/null && echo "plugin stopped" || true
	@$(MAKE) --no-print-directory ps

.PHONY: ps
ps: ## Show running plugin and watcher instances
	@pgrep -fl 'scripts/watch.sh|/$(BINARY)$$' || echo "nothing running"

.PHONY: tabs
tabs: ## Show the current tab names
	@./scripts/probe.py tabs

.PHONY: watch-tabs
watch-tabs: ## Watch the tab names
	@./scripts/probe.py watch-tabs

.PHONY: probe-snapshot
probe-snapshot: ## Show the session snapshot the plugin polls
	@./scripts/probe.py snapshot

.PHONY: clean
clean: ## Remove the built binary
	@rm -f $(BINARY)
