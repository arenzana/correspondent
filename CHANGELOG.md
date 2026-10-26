# Changelog

All notable changes to Correspondent are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Until 1.0, minor versions may
change behaviour; the Google Reader API stays compatible throughout.

## [Unreleased]

## [0.8.0] - 2026-10-26

### Added

- Sites without feeds. Add a page that lists articles and Correspondent
  reads it like a feed, fetching each article in full. It looks for the
  articles exactly first (h-feed microformats, JSON-LD, a repeated link
  pattern; an article's address leads to the listing above it). Where that
  fails, your AI writes a parser for the page: set it up in Settings with
  Anthropic or any OpenAI-compatible service (your key is checked with the
  provider, stored encrypted, and never shown again; up to 20 AI requests a
  day). The model sees an outline of the page and only returns CSS
  selectors, which are checked against the page before they are used;
  checking for new articles never uses AI.
- Parsers are shared: once a page has one, every reader on the server who
  adds it gets it without asking any AI. "Rebuild its parser" in
  Subscriptions repairs a page that changed, for everyone.
- Built in: OpenAI News and Google DeepMind (their feeds, which their pages
  don't advertise), and parsers for Anthropic News, Research and
  Engineering, the Claude blog, xAI, Figure and Groq. Typing "openai.com"
  or "anthropic.com" is enough.
- Reeder can add and manage subscriptions (Google Reader quickadd and
  subscription/edit): sites without feeds work there too, using your AI if
  needed, and arrive as ordinary feeds.
- New feeds whose articles turn out to be summaries fetch full articles
  automatically, so Reeder gets whole articles without any switch.
- Articles fetched in full take their publication date from the page when
  the feed or listing gives none.

## [0.7.4] - 2026-10-26

### Changed

- Softer contrast in every theme, easiest on the eyes at night: text now
  sits around 10-12:1 against its background (from 13-16:1), still well
  past the 7:1 accessibility target.

### Fixed

- On iPhone, switching theme while the app was open (Sun turning to Night
  desk at sunset) left the strip behind the clock in the old colour.

## [0.7.3] - 2026-10-26

### Added

- Interface text in Settings: a text size for the whole app (Small,
  Default, Large, Larger) and its typeface (Grotesk or the system's own,
  San Francisco on Apple devices). Article typeface, text size and line
  length are in Settings too, as well as under the T button.

### Fixed

- Settings could show broken app icons if it was opened while the server
  was restarting; every icon choice is now kept offline by the app.
- No grey-blue flash on buttons when tapped on phones.

## [0.7.2] - 2026-10-26

### Fixed

- Summaries that end with "Continue reading" followed by another line (a
  subscribe link, "The post … appeared first on …") weren't recognised, so
  the article didn't offer the full text. Such links now count anywhere near
  the end, for teasers of up to several paragraphs.

## [0.7.1] - 2026-10-26

### Fixed

- On iPhone the strip behind the clock was the page's background colour, a
  shade off from the header below it. It now matches the header in every
  theme (day, evening, night, and when following the system).

## [0.7.0] - 2026-10-26

### Added

- An image proxy. Article pictures, list thumbnails and feed icons now load
  through Correspondent, so the sites (and their trackers) never see your
  address or what you read. Thumbnails arrive at the size they're shown,
  not as multi-megabyte originals, and pictures are kept for offline reading.
  Addresses are signed, so the proxy fetches only images that articles
  refer to; it uses the same protections as feed fetching, rejects anything
  that isn't an image, and serves SVGs sandboxed. The page now loads images
  from this server only. Cached images live in `imgcache/` beside the
  database (512 MB by default, `CORRESPONDENT_IMAGE_CACHE_MB`; `0` turns the
  proxy off). Reeder is unaffected.
- Full articles for a whole feed, offered where you notice the need: an
  article that looks like a summary now has "Always load full articles from
  <feed>", which turns it on for the feed and loads the open article in full
  (after "Load the full article" too). Summaries are recognised by length
  and by endings such as "…", "Read more" or "Continue reading".
- A tab bar on phones (Feeds, Unread, Starred, Search, Settings), so every
  menu is reachable; it steps aside while an article is open.

### Fixed

- On iPhone the whole page could be dragged, taking the header (and the way
  to the feeds) off screen, or shift an article sideways past the screen's
  edge. Only the list and the article scroll now, and only up and down.

## [0.6.0] - 2026-10-26

### Added

- Full articles for feeds that publish only titles or summaries. Any article:
  "Load the full article" (or the article icon in the toolbar) fetches its
  web page and shows the article itself, with a way back to the feed's text.
  Per feed: "Fetch full articles" (feed header, Subscriptions, ⌘K) fetches
  every new article in full as it arrives, so Reeder and the offline copy get
  the whole text too; turning it on also fetches the latest 20.
- Extraction uses Readability (as in Firefox's Reader View); pages are
  fetched with the same address protections and limits as feeds, and the
  result is sanitized like any feed content.

## [0.5.0] - 2026-10-26

### Added

- Subscriptions page (sidebar, ⌘K, `g m`, or Settings): every feed with its
  address, posting rhythm, last check and any error, unread and total
  counts, grouped by folder. Rename inline, move between folders, change a
  feed's address (it keeps its articles), update now, pause or resume,
  notifications, and delete with a confirmation that says what goes with it.
- Find feeds by name or address; filter to failing, paused, or quiet ones
  (nothing published in 90 days).
- Select several feeds to move, pause, resume or delete together.
- Create, rename and delete folders (deleting a folder keeps its feeds).

## [0.4.0] - 2026-10-26

### Added

- Push notifications, off by default: turn them on per device in Settings,
  then choose feeds with the bell in a feed's header (or ⌘K). Several new
  articles become one notification per feed, replacing the previous one;
  tapping opens the article. Works in Chrome, Edge, Firefox and Safari, and
  on iPhone and iPad for the app added to the Home Screen (iOS 16.4+).
- Settings: send a test notification; see which feeds notify you.

### Changed

- The full app icon now appears beside the wordmark.

### Security

- The server only sends notifications to the browsers' push services over
  HTTPS, so a crafted subscription cannot aim it at other addresses. Gone
  or repeatedly failing subscriptions are removed.

## [0.3.0] - 2026-10-26

### Added

- Installable app (PWA) with offline reading: the feed list, recent lists
  and the full text of unread (up to 600) and starred (up to 300) articles
  are kept on the device, so the reader opens and reads without a network.
- Changes made offline (read, unread, star, unstar, mark all read) are
  queued, shown as "N changes waiting", and sent in order when the
  connection returns; with Background Sync where the browser supports it.
- An "Offline" indicator, and a prompt to reload when a new version is ready
  (a new version never takes over an open reader by itself).
- iPhone home-screen support: edge-to-edge layout clear of the notch and
  home indicator.
- Signing out removes the offline copy and the queue from the device.

## [0.2.0] - 2026-10-26

### Added

- Live sync across clients: reading, starring and mark-all-read from Reeder,
  another tab or another device appear in open readers within a second,
  patched in place without reloading. Readers that fall behind resync.
- The Sun theme has three phases: Day from sunrise to sunset, Evening until
  nautical dusk, then Night desk until sunrise. Where the sky never gets
  dark (summer at high latitudes), Evening lasts until sunrise.
- `GET /api/v1/entries?content=1` returns full articles, for offline sync.

## [0.1.1] - 2026-10-26

### Changed

- The mark beside the wordmark is now the refined "wire" artwork used for
  the app icon, instead of an approximation.

## [0.1.0] - 2026-10-26

The first release: a self-hosted reader that can take over from FreshRSS
without disturbing Reeder.

### Added

- Reader with the wire: articles on a time spine, ordered by publication,
  with unread, read and starred shown on the spine.
- Day, Night desk and Evening themes, plus System and Sun (Evening from
  sunset, computed on the device from a location rounded to about 11 km).
- Keyboard model (j/k, n/p, Space, s, m, v, g-sequences), ⌘K command palette,
  shortcuts sheet, resizable and hideable panes, focused reading.
- Swipe on touch screens: right to toggle read, left to toggle star.
- Mark all read with Undo; read articles stay in place until the list reloads.
- Add feed with discovery from a site address; OPML import and export.
- Paused feeds, with a Resume action.
- App icon choice (Wire, Byline, Morse) for the browser tab, home screen and
  installed app.
- Google Reader API at `/api/greader.php`, compatible with Reeder and other
  clients, matching FreshRSS's identifiers and output.
- FreshRSS migration: `migrate import` from FreshRSS's SQLite export,
  `migrate verify` (go/no-go comparison of both servers), `migrate rollback`
  (replays changes back into FreshRSS), and the static FreshRSS app token
  accepted until `legacy-auth off`. Rehearsed end to end in CI.
- Docker image (distroless, non-root, read-only root, multi-arch, signed),
  Compose file, healthcheck, verified backups (`correspondent backup`).
- Opt-in retention that never removes articles still listed in their feed.

### Security

- Feed fetching refuses private, loopback, link-local and metadata
  addresses on every connection and redirect; size and time limits.
- Article HTML sanitized with an allowlist; fuzzed with a parsing oracle.
- CSRF protection, sign-in throttling for web and app logins, Secure
  session cookies, strict security headers, URL redaction in logs.

[Unreleased]: https://github.com/arenzana/correspondent/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/arenzana/correspondent/compare/v0.7.4...v0.8.0
[0.7.4]: https://github.com/arenzana/correspondent/compare/v0.7.3...v0.7.4
[0.7.3]: https://github.com/arenzana/correspondent/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/arenzana/correspondent/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/arenzana/correspondent/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/arenzana/correspondent/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/arenzana/correspondent/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/arenzana/correspondent/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/arenzana/correspondent/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/arenzana/correspondent/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/arenzana/correspondent/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/arenzana/correspondent/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/arenzana/correspondent/releases/tag/v0.1.0
