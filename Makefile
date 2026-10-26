.PHONY: all ui build test check fuzz vuln dev screenshots image rehearse release clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null)
LDFLAGS := -s -w -X github.com/arenzana/correspondent/internal/version.Version=$(VERSION) -X github.com/arenzana/correspondent/internal/version.Commit=$(COMMIT)

all: build

ui:
	cd ui && npm ci && npm run check && npm run build

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/correspondent ./cmd/correspondent

test:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go test -race -cover ./...
	cd ui && npm test

image:
	docker buildx build --load -t correspondent:$(VERSION) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) .

fuzz:
	go test ./internal/content -run=^$$ -fuzz=FuzzProcess -fuzztime=60s
	go test ./internal/feed -run=^$$ -fuzz=FuzzParse -fuzztime=60s

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd ui && npm audit --audit-level=low

check: test vuln

# Backend on :8080 (plain HTTP, so cookies are not Secure) plus Vite with hot reload on :5173.
dev:
	go run ./cmd/correspondent serve -addr 127.0.0.1:8080 -data ./data -insecure-cookies & cd ui && npm run dev

# The FreshRSS cutover, end to end, against a real FreshRSS in Docker.
rehearse: build
	test/migration/rehearse.sh

# README screenshots from invented fixture content (uses the installed Chrome).
screenshots: build
	cd ui && node scripts/screenshots.mjs

# Tag a release: make release VERSION=0.2.0. Requires a clean tree on main and
# a "## [0.2.0]" section in CHANGELOG.md. Pushing the tag (git push origin
# v0.2.0) builds the signed images and publishes the GitHub release.
release:
	@test -z "$$(git status --porcelain)" || (echo "working tree is not clean" && exit 1)
	@test "$$(git branch --show-current)" = main || (echo "releases are tagged on main" && exit 1)
	@echo "$(VERSION)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "usage: make release VERSION=x.y.z" && exit 1)
	@grep -q '^## \[$(VERSION)\]' CHANGELOG.md || (echo "CHANGELOG.md has no ## [$(VERSION)] section" && exit 1)
	$(MAKE) test
	git tag -a v$(VERSION) -m "Correspondent $(VERSION)"
	@echo "Tagged v$(VERSION). Publish with: git push origin v$(VERSION)"

clean:
	rm -rf bin web/dist/assets web/dist/index.html
