.PHONY: all ui build test check fuzz vuln dev screenshots image rehearse clean

all: build

ui:
	cd ui && npm ci && npm run check && npm run build

build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/correspondent ./cmd/correspondent

test:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go test -race -cover ./...
	cd ui && npm test

image:
	docker buildx build --load -t correspondent:dev --build-arg VERSION=$$(git describe --tags --always --dirty) .

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

clean:
	rm -rf bin web/dist/assets web/dist/index.html
