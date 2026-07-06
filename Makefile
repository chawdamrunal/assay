.PHONY: web build install test lint clean verify-embed

WEB_DIR     := web
DIST_SRC    := $(WEB_DIR)/dist
DIST_DEST   := internal/api/dist
GO          := /opt/homebrew/opt/go/bin/go
GOLANGCI    := PATH=/opt/homebrew/opt/go/bin:$$PATH golangci-lint

# Where `make install` copies the binary. Override for a system-wide install:
#   make install PREFIX=/usr/local
PREFIX      ?= $(HOME)/.local

# Build the React SPA and copy it to the embed location.
web:
	cd $(WEB_DIR) && pnpm install --frozen-lockfile && pnpm build
	rm -rf $(DIST_DEST)
	mkdir -p $(DIST_DEST)
	cp -r $(DIST_SRC)/. $(DIST_DEST)/
	@$(MAKE) verify-embed

# Assert the embed artifact actually landed.
verify-embed:
	@test -f $(DIST_DEST)/index.html || (echo "ERROR: $(DIST_DEST)/index.html is missing — embed copy failed" && exit 1)
	@echo "OK: $(DIST_DEST)/index.html present"

# Build the Go binary (depends on web/ being built and copied).
build: web
	mkdir -p bin
	$(GO) build -o bin/assay ./cmd/assay
	@echo "Built: $$(du -h bin/assay | cut -f1) $$(pwd)/bin/assay"

# Install the built binary onto your PATH (defaults to ~/.local/bin).
install: build
	mkdir -p $(PREFIX)/bin
	cp bin/assay $(PREFIX)/bin/assay
	@echo "Installed: $(PREFIX)/bin/assay"

# Test runs everything: Go + frontend typecheck.
test:
	$(GO) test -race ./...
	cd $(WEB_DIR) && pnpm lint

# Lint runs Go lint + frontend lint.
lint:
	$(GOLANGCI) run ./...
	cd $(WEB_DIR) && pnpm lint

# Clean removes built artifacts (keeps node_modules for speed).
clean:
	rm -rf bin/ $(DIST_DEST) $(DIST_SRC)
