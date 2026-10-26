# corr: Correspondent from the terminal

`corr` reads and manages your Correspondent from a terminal. Run it with no
arguments for a full-screen reader; every other command is made for scripts
and AI agents as much as for people.

## Install

```sh
brew install arenzana/tap/corr
```

or download a binary for macOS or Linux from the
[releases](https://github.com/arenzana/correspondent/releases), or
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
| `o` | open the original in your browser |
| `/` | search; `esc` clears it |
| `tab`, `h` | feeds and folders |
| `A` `A` | mark everything here read |
| `r` | refresh |
| `?` | all keys |
| `q` | quit |

Links in an article are numbered (`text [1]`) and listed at its end.

## Commands

| Command | |
|---|---|
| `corr list [--view unread\|starred\|today\|all] [--feed ID] [--folder ID] [--search TEXT] [--limit N] [--cursor C] [--oldest]` | articles, newest first (unread by default) |
| `corr read ID [--full] [--html] [--keep-unread]` | an article, as Markdown; marks it read unless `--keep-unread` |
| `corr mark read\|unread\|star\|unstar ID…` | change articles; `-` reads IDs from stdin, one per line |
| `corr mark-all-read [--feed ID \| --folder ID \| --view today]` | everything in a feed, folder, today, or all |
| `corr feeds`, `corr folders` | subscriptions and folders with unread counts |
| `corr discover URL [--ai]` | the feeds a site offers (with `--ai`, let your AI read a site without one) |
| `corr subscribe URL [--title T] [--folder F] [--ai]` | subscribe (the first feed found) |
| `corr unsubscribe FEED-ID` | unsubscribe; its articles are deleted |
| `corr feed FEED-ID rename T \| move FOLDER \| unfile \| pause \| resume \| refresh \| full-text on\|off \| notify on\|off \| url ADDRESS` | change a feed |
| `corr opml export [FILE]`, `corr opml import FILE\|-` | OPML |
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

Tools: `list_articles`, `read_article` (Markdown; doesn't mark read unless
asked), `set_article_state`, `mark_all_read`, `list_feeds`, `find_feeds`,
`subscribe`, `unsubscribe`, `update_feed`. Tools that change things are
marked as such, and the destructive ones (`unsubscribe`, `mark_all_read`)
as destructive, so clients can ask you first. The server enforces the
token's scope; article text is presented to models as content, never as
instructions.
