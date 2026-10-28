# corr: Correspondent from the terminal

`corr` reads and manages your Correspondent from a terminal. Run it with no
arguments for a full-screen reader; every other command is made for scripts
and AI agents as much as for people.

## Install

```sh
brew install arenzana/tap/corr   # macOS and Linux
sudo pacman -S corr              # Arch Linux and Omarchy, after adding the repository below
```

On Arch Linux and Omarchy, add corr's repository to `/etc/pacman.conf`
once; after that corr updates with the rest of the system (`pacman -Syu`):

```ini
[correspondent]
SigLevel = Optional TrustAll
Server = https://arenzana.github.io/correspondent/arch/$arch
```

The packages aren't signed: like the Homebrew tap, they come over HTTPS
from this project's GitHub Pages (x86_64, aarch64 and armv7h).

Or download a binary for macOS, Linux (including 32-bit ARM), Windows or
FreeBSD from the [releases](https://github.com/arenzana/correspondent/releases), or
`go install github.com/arenzana/correspondent/cmd/corr@latest`.

## Sign in

```sh
corr auth login --server https://rss.example.org
```

asks for your username and password and saves a personal API token (only
the token) in `~/.config/corr/config.json`, readable only by you. Add
`--read-only` for a token that can read but never change anything. You can
also create tokens in the app (Settings → Account) and use them through the
environment:

```sh
export CORR_SERVER=https://rss.example.org CORR_TOKEN=corr_…
```

Without a terminal, `corr auth login` needs `--server`, `--username` and
`--password-stdin`. `corr auth logout` revokes the token and forgets it;
`corr auth status` shows who you are signed in as.

## The reader

`corr` alone opens it. It follows your theme in the app (Day, Evening,
Night desk; System and Sun use the terminal's background), or pass
`--theme day|evening|night`.

| Key | |
|---|---|
| `j` `k`, `↓` `↑` | next / previous article |
| `enter`, `l` | open the article |
| `space` | page down; at the end, the next article |
| `n` `p` | next / previous article while reading |
| `s` | star or unstar |
| `m` | mark read or unread |
| `f` | fetch the full article from its page |
| `S` | the article's summary, above its text (`S` again hides it); made by your AI only if no one has had one made |
| `l` (reading) | read the article aloud in the background; `l` again stops |
| `o` | open the original in your browser |
| `/` | search titles and text; `esc` clears it |
| `tab`, `h` | feeds, folders and saved searches |
| `A` `A` | mark everything here read |
| `r` | refresh |
| `?` | all keys |
| `q` | quit |

Links in an article are numbered (`text [1]`) and listed at its end.

The reader is live: articles you read or star anywhere (the web app,
Reeder, another terminal) update as it happens, and new articles appear at
the top of the list as they arrive. Further down the list it tells you
instead of moving.

## Commands

| Command | |
|---|---|
| `corr list [--view unread\|starred\|today\|all] [--feed ID] [--folder ID] [--search TEXT] [--saved ID] [--limit N] [--cursor C] [--oldest]` | articles, newest first (unread by default); `--search` finds words in titles and text, `--saved` uses a saved search |
| `corr read ID [--full] [--html] [--keep-unread]` | an article, as Markdown; marks it read unless `--keep-unread` |
| `corr mark read\|unread\|star\|unstar ID…` | change articles; `-` reads IDs from stdin, one per line |
| `corr mark-all-read [--feed ID \| --folder ID \| --view today] [--search TEXT \| --saved ID]` | everything in a feed, folder, today, or all; with a search, only what matches |
| `corr feeds`, `corr folders` | subscriptions and folders with unread counts |
| `corr discover URL [--ai]` | the feeds a site offers (with `--ai`, let your AI read a site without one) |
| `corr subscribe URL [--title T] [--folder F] [--ai]` | subscribe (the first feed found) |
| `corr unsubscribe FEED-ID` | unsubscribe; its articles are deleted |
| `corr feed FEED-ID rename T \| move FOLDER \| unfile \| pause \| resume \| refresh \| full-text on\|off \| notify on\|off \| url ADDRESS` | change a feed |
| `corr opml export [FILE]`, `corr opml import FILE\|-` | OPML |
| `corr watch [--feed ID] [--folder ID] [--notify]` | new articles as they arrive, until Ctrl-C; with `--json`, one JSON object per line; `--notify` also shows desktop notifications |
| `corr listen ID [-o FILE]` | the article read aloud: played with the system's player (afplay, mpv, ffplay or mpg123), or saved with `-o` (`-` for stdout). Uses existing audio, or makes it with your text-to-speech key (Settings → AI) |
| `corr summary ID` | the article's short summary: the shared one, or written now by your AI (Settings → AI) |
| `corr send [ID] [--to NAME]` | sends the article to one of your services (Settings → Integrations) and says when it's there; with no ID, lists them |
| `corr searches` | saved searches with unread counts; `corr searches save WORDS [--name N]`, `rename ID NAME`, `remove ID` |
| `corr version` | |

Flags work before or after the command: `--json`, `--server URL`,
`--token TOKEN`, `--theme NAME`.

## JSON

With `--json`, every command prints one JSON object on stdout. Fields are
only ever added, never renamed or removed. Errors go to stderr as
`{"error": "…", "code": N}`.

```jsonc
// corr list --json
{ "entries": [ { "id": "1793046108817442", "title": "…", "feed": "…", "feedId": 2,
    "url": "https://…", "excerpt": "…", "publishedAt": "2026-10-26T00:00:00Z",
    "read": false, "starred": false } ],
  "cursor": "1792627200.1793046108817441" }   // "" at the end

// corr read ID --json
{ "id": "…", "title": "…", "author": "…", "url": "…", "feedId": 2, "publishedAt": "…",
  "read": true, "starred": false, "fullText": false, "markdown": "…" }

// corr feeds --json
{ "feeds": [ { "id": 2, "title": "…", "url": "…", "folder": "AI & tech", "unread": 8, "total": 40,
    "paused": false, "fullText": true, "page": true, "notify": false, "error": "", "latestAt": "…" } ] }

// corr watch --json: one object per line, as articles arrive
{"id":"…","title":"…","feed":"…","feedId":2,"url":"…","excerpt":"…","publishedAt":"…"}

// corr mark … --json, corr mark-all-read --json
{ "action": "star", "changed": 1 }
```

For example, mark everything from one feed older than a week as read:

```sh
corr list --feed 4 --view unread --limit 100 --json \
  | jq -r --arg d "$(date -v-7d -u +%FT%TZ)" '.entries[] | select(.publishedAt < $d) | .id' \
  | corr mark read -
```

## Exit codes

| Code | |
|---|---|
| 0 | ok |
| 1 | error |
| 2 | usage: wrong command or flags |
| 3 | not signed in, or not allowed (a read-only token writing) |
| 4 | not found |
| 5 | server unavailable or busy (retry later) |

## AI tools (MCP)

Correspondent serves the Model Context Protocol itself, at
`https://<your server>/mcp` (Streamable HTTP). Connect with a token from
Settings → Account; a read-only token is the safe choice for an agent.

```sh
claude mcp add --transport http correspondent https://rss.example.org/mcp \
  --header "Authorization: Bearer corr_…"
```

Tools: `list_articles` (search finds words in the text; or a saved search),
`read_article` (Markdown; doesn't mark read unless asked),
`summarize_article` (the shared summary, or written by your AI),
`set_article_state`, `mark_all_read` (optionally only a search's matches),
`list_feeds` (with saved searches), `save_search`, `remove_search`,
`find_feeds`, `subscribe`, `unsubscribe`, `update_feed`, `list_integrations`,
`send_article` (to one of your services). Tools that change
things are marked as such, and the destructive ones (`unsubscribe`,
`mark_all_read`, `remove_search`) as destructive, so clients can ask you
first. The server enforces the
token's scope; article text is presented to models as content, never as
instructions.
