# Changelog

All notable changes to Correspondent are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Until 1.0, minor versions may
change behaviour; the Google Reader API stays compatible throughout.

## [Unreleased]

## [0.25.0] - 2026-10-29

### Added

- Settings, Reading: **Show shortcuts** puts each key on the button it
  presses (Star `s`, Summary `S`, the lists' `g u`, `g t`…), and every
  such button names its key in its tooltip. On the web, the Mac and
  iPad; not on phones.
- Settings, Reading: **Emacs keys** adds Emacs's bindings beside the
  single letters: Control-N and Control-P for the next and previous
  article, Meta-N and Meta-P for unread ones, Control-V and Meta-V to
  page, Meta-< and Meta-> for the beginning and end of the article (or
  of the list when no article is open), Control-S to search, Control-G
  to close. Meta is Option on a Mac. The shortcuts sheet (`?`) lists
  them while they're on.

### Fixed

- On the Mac, with the sidebar hidden, the bar under the list is the
  list's white instead of the sidebar's grey.

## [0.24.2] - 2026-10-29

### Changed

- `corr` shows pictures sharp in terminals that draw images themselves
  (Ghostty, Kitty, and herdr inside them): it sends the image, and the
  terminal draws it at full resolution where the picture goes, scrolling
  with the text. Other terminals keep the half-block drawing.
  `CORR_IMAGES=blocks` or `CORR_IMAGES=kitty` chooses one way or the
  other.

## [0.24.1] - 2026-10-29

### Fixed

- Links to a site's other stories set in the middle of an article
  ("Read more: “A headline”", "Related: …", "See also: …") are
  removed, wherever they are; a "Read more" closing a summary stays.
  Stored articles are tidied again on the next start.
- On the Mac, adding a subscription shows every folder: the folder
  choices wrap onto more lines instead of scrolling sideways out of
  reach.

## [0.24.0] - 2026-10-29

### Added

- `corr` shows articles' pictures: in the reader and in `corr read` on a
  terminal, each image that stands on its own is drawn in colour, in
  half blocks, so it shows in any terminal, tmux and the like included.
  Pictures load after the text in the reader, so an article still opens
  at once. Images inside a sentence, tracking pixels, and ones that
  can't be loaded keep their label. `corr read --no-images` turns them
  off; piped or `--json` output is unchanged. `corr read` on a terminal
  numbers its links and lists them at the end, as the reader does.

## [0.23.6] - 2026-10-29

### Fixed

- On the Mac, the window keeps its size and place across launches and
  updates. macOS saved them under a name that changed with every build,
  so each update forgot them. (The first launch of this version still
  opens at the default size.)

## [0.23.5] - 2026-10-28

### Fixed

- A service added, changed or removed on the web shows in the apps' Send
  to menu at once, without restarting them: the server tells open readers
  (a new `integrations` live event), and the apps also look again when
  they come back to the front. Other open tabs follow too.

## [0.23.4] - 2026-10-28

### Fixed

- The command-line tool's release: signing its checksums retries again
  without losing its arguments, which kept 0.23.3 from publishing `corr`.

## [0.23.3] - 2026-10-28

### Changed

- "All caught up" has a small drawing: the paper read, a cup of coffee
  still steaming (the steam stays still with reduced motion). On the web
  and in the apps.

## [0.23.2] - 2026-10-28

### Fixed

- The end of the article list: the spine simply stops at the last
  article, on the web and in the apps, with no stray bar below it.

## [0.23.1] - 2026-10-28

### Fixed

- The end of the article list: the spine runs on into its end bar.

## [0.23.0] - 2026-10-28

### Added

- One way of reading: every article, from its feed or its page, is
  tidied into the same shape. Wrappers and empty blocks go, headings run
  in order from just below the title (and one that only repeats the title
  goes), a standfirst shown twice shows once, and the site's own material
  at either end goes: navigation, bylines and dates, subscribe and share
  prompts, tags, affiliate notices, newsletter boxes and "Related
  articles" lists. "Read more" links stay, since they mark a summary.
  Articles already stored are tidied once, in the background.
- `CORRESPONDENT_TYPESAFE_KEY` (or `_FILE`): with a TypeSafe key, the
  server asks Jev about the blocks at each new article's start and end,
  for the site material no rule recognises (membership plugs, author
  bios), removing it from the outside in and stopping at the first block
  it isn't sure of. Optional; without a key, the rules alone tidy.

## [0.22.1] - 2026-10-28

### Fixed

- Link blogs (Daring Fireball, Lex on Tech): a post about another site's
  article no longer has its commentary replaced by that article when full
  articles are on. Such posts are recognised by their own page in the
  feed, and the ones already stored get their commentary back on the next
  refresh. Aggregators with no commentary (Hacker News) still get the
  linked article.
- Full articles never replace a feed's text with the wrong part of a page
  (Ars Technica's pages extract as an article's second half), and a feed
  that already carries the whole article keeps it unless the page adds
  substantially more, so its own headings and layout stay. "Load the full
  article" still fetches it on request.

## [0.22.0] - 2026-10-28

### Added

- Integrations: send any article to Readwise Reader, Instapaper,
  Raindrop.io, your own Wallabag, Linkding or Karakeep, or a webhook.
  Connect them in Settings → Integrations (credentials are checked with
  the service and stored encrypted); send with `b`, the send button, the
  palette, the apps' paper plane, `corr send` or the MCP `send_article`
  tool. The server sends each article and retries while the service is
  down, and says where it went. Each service can also get every article
  you star, from any app, Reeder included.
- Webhooks get each article as JSON with its Markdown, signed with
  `Correspondent-Signature: t=…,v1=…` (HMAC-SHA256 of "t.body").
- Send to Obsidian, on the device: the article as a Markdown note with
  its title, source, author and dates as properties, in the vault and
  folder set in Settings.
- `CORRESPONDENT_INTEGRATIONS_ALLOW`: your own networks (a tailnet) that
  integrations may reach, with plain `http://` allowed there.

## [0.21.3] - 2026-10-28

### Security

- Summaries and audio made through an address a reader entered (their own
  OpenAI-compatible server) stay that reader's; only those from a
  provider's own service are shared. A reader can no longer rebuild a page
  parser that still works for others; admins still can.
- A new app password signs out Reeder sessions made with the old one; an
  admin's password reset signs out everything, API tokens included.
- Sign-in limits count each try as it's let in, so parallel guesses can't
  get past them.
- The image proxy won't decode an image that would take more than 100 MB,
  and decodes two at a time.
- A feed can only be filed in its own reader's folders.
- A disabled account's audio links stop working.
- golang.org/x/net 0.62.0 (GO-2026-6866, GO-2026-6860).

### Fixed

- Web:
  - A session ended elsewhere returns to signing in.
  - Live updates come back after a server restart.
  - The offline copy opens when the server is down behind its proxy.
  - New articles slot into the list without moving rows already read.
  - An article arriving while the list loads is no longer missed (or
    marked read unseen by Mark all read).
  - Escape in the reading settings leaves the article open.
  - Space on a control reached with the keyboard presses it.
  - Shortcuts are off behind any dialog.
  - Hidden panes are out of Tab order.
  - Settings changed offline are sent later rather than lost.
  - The offline copy downloads only new or changed articles.
  - Only the app's page is kept as the offline shell.
  - Signing out also clears cached pictures and saved settings.
- Apple apps:
  - Lists keep their place when articles arrive, and no longer mix when
    switching quickly.
  - Signing out unregisters notifications, stops audio and resets the
    list.
  - The Mac app starts once and has one window.
  - The offline copy is written far less often.
  - Widgets refresh with the counts.
  - iPhone tabs show their own lists.
  - Add feed works from the Search tab.
  - Escape in a Mac sheet closes the sheet.
  - Saved searches work offline.
  - Smaller fixes, and VoiceOver labels.
- Scripted scrolling follows Reduce Motion everywhere.

## [0.21.2] - 2026-10-28

### Security

- Admin routes refuse API tokens and take a signed-in session only. An
  admin's API token could otherwise mint a password-reset link for its own
  account and take it over, or invite another admin.

### Fixed

- Apple apps: live updates from other devices arrive while the app is
  open, rather than only after reconnecting.
- iPhone: opening an article from a notification or widget while on
  another tab shows the article rather than an empty page.

## [0.21.1] - 2026-10-28

### Fixed

- The Sun theme follows the local day rather than the UTC one. In the
  Americas an evening counted as the next UTC day and showed Night desk
  before sunset; east of Greenwich the next change could fall in the past.

### Added

- The Apple apps ask for a rough location when the Sun theme is chosen, as
  the web does, rounded to about 11 km; refusing keeps the estimate from the
  time zone.

## [0.21.0] - 2026-10-27

### Changed

- The Sun theme turns to Evening an hour before sunset and to Night desk 45
  minutes after it (it used to wait for sunset, then nautical dusk), on the
  web and in the Apple apps alike.

## [0.20.1] - 2026-10-27

### Changed

- Bionic reading uses semibold rather than bold, and the rest of each word
  is a shade lighter: heavy word starts read as blobs, Newsreader most of
  all.

## [0.20.0] - 2026-10-27

### Added

- Bionic reading: the start of each word in bold, for the eye to jump
  from word to word (code is left alone). In the text controls and
  Settings.
- The spotlight: one paragraph, or one line, stays clear while the rest
  of the article is dimmed. ↓ and ↑ move it a paragraph or a line at a
  time; scrolling moves it too. In the web app and the Apple apps.

## [0.19.0] - 2026-10-27

### Changed

- Text sizes are kept per device: article text size, line length and
  interface text size live in each browser (and each app), so a phone and
  a desk can differ. A device starts from the account's sizes the first
  time.
- Preferences save only what changed (`PATCH /api/v1/prefs`, a JSON merge
  patch), so a tab left open no longer puts back another device's older
  settings. `PUT` still replaces them all.

## [0.18.1] - 2026-10-27

### Added

- A small bell next to feeds that notify you, in the sidebar.

## [0.18.0] - 2026-10-27

### Added

- The Apple apps grow up: adding and managing feeds, folders and saved
  searches; an offline outbox; read-aloud with background audio and
  lock-screen controls; background refresh and the unread count on the
  icon; subscribing from the share sheet; widgets; Shortcuts and Siri;
  Mac menus with shortcuts; long-press menus on articles; text controls,
  focused reading, next and previous, and picture zoom; Dynamic Type and
  VoiceOver throughout. The Mac app is attached to this release, signed
  with Developer ID and notarized.
- Notifications for the Apple apps through Apple's push service. With an
  APNs key (CORRESPONDENT_APNS_KEY_FILE, _KEY_ID, _TEAM_ID; _TOPIC
  defaults to org.arenzana.correspondent) the server sends feed
  notifications, and the test notification, to the apps' devices as well
  as to browsers, and forgets devices Apple says are gone.

## [0.17.2] - 2026-10-27

### Added

- The start of native apps for iPhone, iPad and Mac (`apple/`), with the
  web app's design: sign-in with a device token, the feeds sidebar, the
  wire with its time spine, the article with the web's styles, search,
  live updates and an offline copy. iPhone uses iOS's own tab bar and
  navigation. Not yet released.

### Fixed

- A Mark all read made offline inside a search marked the whole list read
  when it synced, not only the search's matches.
- `correspondent invite` failed with a foreign-key error: an invitation
  made from the command line has no creating account.

## [0.17.1] - 2026-10-27

### Changed

- On phones, scrolling down a list tucks its header and the tab bar away,
  and scrolling down an article tucks its toolbar; scrolling up, even a
  little, brings them back. Lists too short to need the room keep them.

### Fixed

- The back arrow in Settings (and Admin) did nothing in the installed app
  or a wide window: it now goes back to your articles everywhere.
- Notifications in Brave: Brave turns web push off by default and only
  reports a vague error. Settings now says which Brave setting to turn on
  ("Use Google services for push messaging").

## [0.17.0] - 2026-10-27

### Added

- corr: `corr summary ID`, and `corr searches` to list, save, rename and
  remove saved searches; `corr list --saved ID` and
  `corr mark-all-read --search/--saved` (only the matches are read).
- corr's reader: `S` shows the article's summary above its text, `l`
  reads it aloud in the background (`l` again stops), and saved searches
  sit with the feeds and folders. Search there finds words in the text.
- AI tools (MCP): `summarize_article`, `save_search` and `remove_search`;
  `list_feeds` includes saved searches, and `list_articles` and
  `mark_all_read` take a saved search.

### Fixed

- `corr mark-all-read` always said it had marked 0 articles (it did mark
  them).

## [0.16.0] - 2026-10-27

### Added

- Saved searches. After a search, Save search keeps it in the sidebar under
  Searches, with its unread count, like a folder; it follows new articles
  as they arrive, on every device. Rename or remove it from its list
  (removing can be undone). They're in the command palette too.
- A search shows what you searched for in the list header, with Save
  search and a button to clear it.

### Changed

- The selection's ripple is slower and gentler, settling over almost a
  second.

### Fixed

- Mark all read while searching marked the whole list read, not only the
  articles matching the search.

## [0.15.2] - 2026-10-27

### Changed

- The selection's blue line ripples as it moves between articles, like a
  plucked string, and settles straight when it lands. With Reduce Motion
  it stays straight.

## [0.15.1] - 2026-10-27

### Changed

- The sidebar folds with an arrow beside the logo (or `[`). Folded, the
  logo row and the toolbar (add feed, shortcuts, subscriptions, settings)
  glide over to the article list, so they're always on screen.

### Fixed

- A list row shows the article's picture as soon as the server finds one
  (when its full article is fetched, for instance), instead of only after
  the list reloads.

## [0.15.0] - 2026-10-27

### Added

- Summaries: `Shift+S`, the summary button or the command palette shows a
  short summary of the article (two or three sentences), written by your
  own AI only when you ask. Summaries are shared: once anyone has had an
  article summarized, everyone with the same article reads it without
  using their AI. Up to 100 new summaries a day per reader.
- Search finds articles by their text, not only titles and excerpts.
  Matching articles appear as you type in the search box (`⌘K`, `/`, or the
  Search tab), and offline search covers the text of articles kept on the
  device. Articles already stored are indexed when the server starts.
- Search in Settings.
- Settings has an AI section, with your AI model (summaries and sites
  without feeds) and read-aloud together.
- A sponsored post now asks whether you'd rather not see posts like it,
  with "Skip sponsored posts" and "Keep showing them".
- `Esc` closes the open article.

### Fixed

- New articles now appear in the list (animated in at the top, or offered
  as "N new articles" further down) however the reader learns of them.
  Before, coming back to the app on a phone updated the unread count but
  left the list as it was, even an empty "All caught up" list.

## [0.14.1] - 2026-10-27

### Added

- A pacman repository for corr, for Arch Linux and Omarchy (x86_64, aarch64,
  armv7h), published with each release. Add it to `/etc/pacman.conf` once
  (see docs/cli.md), then `sudo pacman -S corr`; it updates with the system.

## [0.14.0] - 2026-10-27

### Added

- Listen to a list: `Shift+L`, the headphones above the list, or the
  command palette plays the list's unread articles that already have audio,
  one after another, starting from the open one; each counts as read when
  it ends. It never makes audio: that happens only for an article you pick.
  Phones' lock screens get a "next" control.
- Listen offline. Audio for what you're listening to, what's queued, and the
  newest 25 unread or starred articles that have it is kept on the device,
  and a saved copy plays with no network and starts at once. Turn it off in
  Settings → This device; nothing is downloaded when the device is saving
  data. Signing out forgets it.

### Fixed

- On iPhones, a stray piece of the list's timeline no longer shows above
  the tab bar when the list is empty or at its end.

## [0.13.0] - 2026-10-26

### Added

- Listen to articles. Set up your own text-to-speech key in Settings →
  Reading (OpenAI or a compatible service, ElevenLabs, or Deepgram; English
  voices), then press `l` or the headphones in an article. A player stays
  at the bottom while you browse, with speed, skip and lock-screen
  controls. The article's prose is read: title, author and text, without
  code, tables, captions or images.
- Read-aloud audio is shared: made once with one reader's key, then
  playable by everyone who follows that article, without a key. Apps like
  Reeder get it as the article's audio through a private link.
- `corr listen ID` plays an article read aloud, or saves it with `-o`.

## [0.12.0] - 2026-10-26

### Added

- Skip sponsored posts (Settings → Reading). Posts titled "Sponsor:",
  "[Sponsor]", "Sponsored by" and the like, or tagged as sponsored in the
  feed, arrive already read, on the web, in Reeder and in corr, and never
  notify. Turning it on also clears the ones waiting. They stay in All.
- corr for Windows (amd64, arm64), FreeBSD (amd64, arm64) and 32-bit ARM
  Linux, besides macOS and Linux. On Windows it opens links in the default
  browser, `corr watch --notify` shows toasts, and the config lives in
  %AppData%\corr.

## [0.11.0] - 2026-10-26

### Added

- corr, the command line: a full-screen terminal reader (Bubble Tea) in
  the web app's themes (Day, Evening, Night desk, following your choice
  there), with articles rendered as Markdown and links numbered at the end;
  and commands to list, read, mark, subscribe and manage feeds and folders,
  discover feeds (with AI for sites without them) and import or export
  OPML. Every command prints stable JSON with --json, never prompts (except
  signing in on a terminal), and exits with codes that say what went wrong.
  Install with `brew install arenzana/tap/corr` or from the release.
- corr is live: the reader shows reads, stars and new articles from
  anywhere as they happen, and `corr watch` prints new articles as they
  arrive (one JSON object per line with --json; desktop notifications with
  --notify).
- MCP for AI tools at /mcp (Streamable HTTP): list and read articles (as
  Markdown), mark them, list, find, add, change and remove feeds. Tools are
  marked read-only or destructive so clients can ask first, and reading
  never marks articles read unless asked.
- Personal API tokens (Settings → Account, or corr auth login): read-only or
  read-and-change, shown once, revocable, listed with when they were last
  used. Tokens can't change a password, passkeys, other tokens or the AI
  key, and stop working when an account is disabled.

## [0.10.0] - 2026-10-26

### Added

- Several readers on one server. The first account is the admin; others
  join through invitation links (one-time, valid for a week) where they
  choose their own username and password. Everyone has their own feeds,
  state, settings, Reeder password and AI key; shared parsers and the image
  cache serve everyone.
- Admin page: accounts (make admin, disable, password-reset link, delete
  with all data; the last admin can't be removed), pending invitations,
  server health (feeds failing across accounts, database and image-cache
  size, last backup, polling), and shared parsers (test, rebuild, remove).
- Passkeys: add them in Settings → Account and sign in with Face ID, Touch
  ID or a security key, offered right in the username field where the
  browser can. Needs `CORRESPONDENT_PUBLIC_URL`.
- Settings → Account: change your password (other browsers are signed out)
  and see and sign out the devices and apps signed in to your account.
- `correspondent invite [-admin]` and `correspondent user role` on the
  command line.

### Security

- A disabled account can't sign in, its sessions and app tokens stop
  working at once, and its feeds stop updating. Signing in as a different
  reader on a device first erases the previous reader's offline copy.

## [0.9.0] - 2026-10-26

### Added

- The selection moves fluidly along the list: one highlight travels from
  article to article, its leading edge first and the trailing edge
  following, so it stretches and settles instead of jumping.
- Settings is a page of its own (Settings in the sidebar or tab bar, or
  press `,`), in five sections: Appearance, Reading, Feeds, Apps & sync and
  About, with a rail that follows the page (section buttons on phones).
  Live previews show the list and an article as theme, text size,
  typefaces, density and line length change. Feeds gathers your
  subscriptions at a glance, OPML and AI; Apps & sync has Reeder's details
  with a copy button, notifications, and this device's offline copy. New
  setting: show only unread articles in feeds and folders.
- New articles slide in at the top of the list as they arrive, pushing it
  down, when you're at the top. Further down, or while you're touching the
  list, nothing moves: the "new articles" button appears instead. Reduced
  motion turns both animations off.

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

[Unreleased]: https://github.com/arenzana/correspondent/compare/v0.11.0...HEAD
[0.11.0]: https://github.com/arenzana/correspondent/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/arenzana/correspondent/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/arenzana/correspondent/compare/v0.8.0...v0.9.0
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
