# Correspondent

A self-hosted feed reader with a calm, keyboard-first reading experience, and
a drop-in replacement for FreshRSS as far as Reeder and other Google Reader
clients are concerned.

- **The wire.** Articles hang off a time spine: a filled dot is unread, a ring
  is read, a gold seal is starred. Three themes (Day, Night desk, Evening),
  Newsreader for reading, Schibsted Grotesk for the interface.
- **Reeder-compatible.** `/api/greader.php` serves the Google Reader API with
  FreshRSS's identifiers and timestamps, so an existing Reeder account keeps
  working after cutover.
- **One binary, one container.** Go service with an embedded Svelte 5 UI and
  SQLite, shipped as a 27 MB signed, distroless image.
- **Follows the sun.** The Sun theme switches to Evening at sunset where you
  are, computed on your device from a location rounded to about 11 km.
- **Swipe on phones.** Swipe right to mark read, left to star.

![The wire in Day theme: feeds, the time spine, and an open article](docs/screenshots/desktop-day.png)

| Night desk | Evening, focused reading |
|---|---|
| ![Night desk theme](docs/screenshots/desktop-night.png) | ![Evening theme in focused reading mode](docs/screenshots/reading-evening.png) |
| **Jump anywhere with ⌘K** | **Add a feed from a site or feed address** |
| ![Command palette](docs/screenshots/command-palette.png) | ![Add feed dialog with a discovered feed](docs/screenshots/add-feed.png) |

<p>
  <img src="docs/screenshots/mobile-wire.png" alt="The wire on a phone" width="250">
  <img src="docs/screenshots/mobile-swipe.png" alt="Swiping a row to mark it read" width="250">
  <img src="docs/screenshots/mobile-article.png" alt="An article on a phone" width="250">
</p>

Screenshots use invented feeds and are regenerated with `make screenshots`.

## Run with Docker

```sh
mkdir -p secrets
openssl rand -base64 24 > secrets/password       # web sign-in
openssl rand -base64 24 > secrets/api_password   # Reeder and other apps
docker compose up -d
```

The image (`ghcr.io/arenzana/correspondent`, amd64 and arm64) is distroless,
runs as a non-root user, works with a read-only root filesystem, and is
signed with cosign:

```sh
cosign verify ghcr.io/arenzana/correspondent:latest \
  --certificate-identity-regexp 'https://github.com/arenzana/correspondent/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The first start creates the account named by `CORRESPONDENT_USER` from the
secret files; passwords are only ever read from files. Put it behind a
TLS-terminating proxy and set `CORRESPONDENT_TRUSTED_PROXIES` to the proxy's
range. Useful commands inside the container:

```sh
docker compose exec correspondent /correspondent backup /data/backup-$(date +%F).db
docker compose exec correspondent /correspondent version
```

| Variable | Default | Purpose |
|---|---|---|
| `CORRESPONDENT_ADDR` | `:8080` | Listen address |
| `CORRESPONDENT_DATA` | `/data` | SQLite directory (local disk, not NFS) |
| `CORRESPONDENT_USER` | | Account to create on first start |
| `CORRESPONDENT_PASSWORD_FILE` | | Web password file for that account |
| `CORRESPONDENT_API_PASSWORD_FILE` | | App (Reeder) password file |
| `CORRESPONDENT_TRUSTED_PROXIES` | | CIDRs whose `X-Forwarded-For` is trusted |
| `CORRESPONDENT_RETENTION_DAYS` | `0` (keep all) | Delete read, unstarred articles older than this that have left their feed |
| `CORRESPONDENT_NO_POLL` | | `1` to serve without fetching feeds (used during migration) |
| `CORRESPONDENT_INSECURE_COOKIES` | | `1` to allow plain HTTP (development only) |

## Build from source

```sh
make build                                   # UI + Go binary in bin/
./bin/correspondent user add <name>          # reads the password from stdin
./bin/correspondent user api-password <name> # separate password for Reeder
./bin/correspondent serve -addr :8080 -data ./data
```

Session cookies are `Secure` by default, so serve it over HTTPS (or on
localhost). Pass `-insecure-cookies` only for plain-HTTP development.
Behind a reverse proxy, pass `-trusted-proxies 10.42.0.0/16` (your ingress
range) so sign-in throttling sees real client addresses.

## Migrating from FreshRSS

Move a FreshRSS account without touching Reeder: same feed and article IDs,
same passwords, the token FreshRSS issued keeps working, no duplicates, and a
tested way back. See [docs/migration.md](docs/migration.md).

```sh
correspondent migrate import isma -export isma.sqlite -system-config config.php -user-config users/isma/config.php
correspondent migrate verify isma -freshrss https://old.example/api/greader.php -correspondent https://new.example/api/greader.php -api-password-file pw
correspondent migrate rollback isma -freshrss https://old.example/api/greader.php -api-password-file pw -apply
```

`make rehearse` runs the whole cutover against a real FreshRSS in Docker.

## Development

```sh
make dev     # Go on :8080, Vite with hot reload on :5173
make test    # gofmt, vet, Go race tests with coverage, UI unit tests (Vitest)
make image   # local Docker image
make fuzz    # fuzz the sanitizer and feed parser
make vuln    # govulncheck + npm audit
```

## Roadmap

- **A complete PWA**, good enough that native apps are unnecessary: offline
  reading of recent articles, read and star changes queued offline and
  replayed through the mutation journal, and push notifications.
- An image proxy that resizes thumbnails and stops article images from
  reaching third parties directly.
- Full-article extraction for feeds that publish only titles or summaries.
- Fluid motion for the selection bar on the time spine as you move up and
  down the list, and new articles sliding in at the top and pushing the list
  down as they arrive (when you're at the top; a list you're reading further
  down stays still).
- **A CLI reader and manager** for people and AI agents alike: the same
  binary talking to `/api/v1` with personal API tokens, `--json` output with
  stable schemas, no prompts, meaningful exit codes, and an MCP server.

## Layout

| Path | Responsibility |
|---|---|
| `internal/fetch` | The only network path: SSRF-safe dialer, size limits, conditional GET |
| `internal/feed`, `internal/content` | Feed normalization, HTML sanitization (bluemonday, applied last) |
| `internal/store` | SQLite schema, hand-written SQL, crawl-time entry IDs, mutation journal |
| `internal/ingest` | Polling scheduler, backoff, discovery, retention |
| `internal/api` | `/api/v1` for the web UI |
| `internal/greader` | Google Reader facade for Reeder |
| `internal/migrate` | FreshRSS import preserving legacy IDs |
| `ui/` | Svelte 5 reader |

## License

MIT. See [LICENSE](LICENSE).
