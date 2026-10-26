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
- **One binary.** Go service with an embedded Svelte 5 UI and SQLite.

![The wire in Day theme: feeds, the time spine, and an open article](docs/screenshots/desktop-day.png)

| Night desk | Evening, focused reading |
|---|---|
| ![Night desk theme](docs/screenshots/desktop-night.png) | ![Evening theme in focused reading mode](docs/screenshots/reading-evening.png) |
| **Jump anywhere with ⌘K** | **Add a feed from a site or feed address** |
| ![Command palette](docs/screenshots/command-palette.png) | ![Add feed dialog with a discovered feed](docs/screenshots/add-feed.png) |

<p>
  <img src="docs/screenshots/mobile-wire.png" alt="The wire on a phone" width="260">
  <img src="docs/screenshots/mobile-article.png" alt="An article on a phone" width="260">
</p>

Screenshots use invented feeds and are regenerated with `make screenshots`.

## Build and run

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

```sh
correspondent import-freshrss <name> \
  -opml feeds.opml.xml -starred starred.json -subscriptions subscription-list.json
```

`subscription-list.json` is the response of
`/api/greader.php/reader/api/0/subscription/list?output=json` from the old
server. It is the only export that carries FreshRSS's numeric feed IDs, which
Reeder has cached; with it, every feed keeps its ID. Starred articles keep
their original item IDs.

## Development

```sh
make dev     # Go on :8080, Vite with hot reload on :5173
make test    # vet + race tests
make fuzz    # fuzz the sanitizer and feed parser
make vuln    # govulncheck + npm audit
```

## Roadmap

- **A complete PWA**, good enough that native apps are unnecessary: offline
  reading of recent articles, read and star changes queued offline and
  replayed through the mutation journal, and push notifications.
- **A CLI reader and manager** for people and AI agents alike: the same
  binary talking to `/api/v1` with personal API tokens, `--json` output with
  stable schemas, no prompts, meaningful exit codes, and an MCP server.
- Cutover tooling for the FreshRSS migration: mutation-journal export and
  reverse replay for rollback, plus a rehearsal against a cloned account.
- An image proxy that resizes thumbnails and stops article images from
  reaching third parties directly.

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
