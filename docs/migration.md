# Migrating from FreshRSS

This moves a FreshRSS account to Correspondent so that Reeder (or any Google
Reader client) carries on without being reconfigured: no new account, no
duplicate articles, no lost read or star state, and a way back.

`make rehearse` runs this whole procedure against a real FreshRSS 1.30.4 in
Docker, with fixture feeds, and fails on any broken invariant. Run it before
touching production.

## What carries over

| From FreshRSS | In Correspondent |
|---|---|
| Feeds, with their numeric IDs | Same IDs, so `feed/<id>` streams are unchanged |
| Categories | Folders with the same names (`user/-/label/<name>`) |
| Every article, with ID, GUID, dates, read and star state | Same, so Reeder's cached items still match |
| Web password and API password (bcrypt hashes) | Same passwords keep working |
| The token FreshRSS issued to apps | Accepted until you turn it off |
| Per-feed refresh interval, and paused feeds | Same; paused feeds stay paused |
| Titles exactly as FreshRSS served them | Same, including FreshRSS's fullwidth ＆ ＜ ＞ and titles derived for untitled articles |

Not migrated: HTTP credentials on feeds (re-add them), XPath/JSON scraping
feeds (history is kept, polling will report errors), user labels other than
categories, extensions and themes. The import lists anything it skipped.

## What you need

- The account name (here `isma`).
- The API password, in a file only you can read (`chmod 600`). It is used to
  compare both servers through the same API Reeder uses.
- Shell access to the FreshRSS host or container.
- Correspondent deployed with an empty data volume, reachable by you but not
  yet behind `rss.arenzana.org` (see the README's Docker section).

## 1. Rehearse on a copy (no downtime)

Do the steps in section 2 without stopping anything and without switching
traffic. FreshRSS keeps fetching, so `migrate verify` may report a few new
articles; anything else it reports is a problem to fix first. Throw the
Correspondent volume away afterwards.

## 2. Cutover (about 10 minutes)

**Freeze FreshRSS.** Close Reeder on every device so nothing syncs, then stop
FreshRSS's scheduled refresh. With the official image that is the `CRON_MIN`
setting; on a plain install, disable the cron entry that runs
`app/actualize_script.php`.

**Export.** On the FreshRSS host (inside the container if it runs in Docker;
`/var/www/FreshRSS` is the image's install path):

```sh
cd /var/www/FreshRSS
sudo -u www-data php cli/export-sqlite-for-user.php --user isma --filename /tmp/isma.sqlite
cp data/config.php /tmp/freshrss-config.php
cp data/users/isma/config.php /tmp/freshrss-user-config.php
```

`config.php` holds the salt that Reeder's token is derived from, and the user
config holds the password hashes. Treat all three files as secrets, copy them
to the Correspondent host over SSH, and delete them afterwards.

**Import** into an empty Correspondent volume, with polling off:

```sh
docker compose run --rm correspondent migrate import isma \
  -export /secrets/isma.sqlite \
  -system-config /secrets/freshrss-config.php \
  -user-config /secrets/freshrss-user-config.php
CORRESPONDENT_NO_POLL=1 docker compose up -d
```

**Verify.** Compare the two servers through the Google Reader API:

```sh
correspondent migrate verify isma \
  -freshrss https://rss.arenzana.org/api/greader.php \
  -correspondent https://correspondent.internal.example/api/greader.php \
  -api-password-file ~/.config/correspondent/api_password
```

It compares subscriptions and folders, the unread and starred ID sets, unread
counts per stream, the most recent history, the fields of a sample of items,
and checks that the token FreshRSS issued is accepted. Continue only on
"All checks passed. Safe to switch over."

**Switch traffic.** Point `rss.arenzana.org` at Correspondent. The path
`/api/greader.php/...` must reach it unchanged:

- no redirects on that path (clients drop POST bodies and auth on redirect)
- pass `Authorization` through untouched
- keep request methods and bodies (a proxy turning POST into GET breaks sync)
- don't decode `%2F` in paths

For nginx:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;              # live updates (server-sent events)
    client_max_body_size 10m;         # OPML imports
}
```

Then set `CORRESPONDENT_TRUSTED_PROXIES` to the proxy's address range.

**Check with Reeder.** Open Reeder on one device and sync. Unread and starred
counts should match what FreshRSS showed, with no duplicates and no sign-in
prompt. Then the second device.

**Resume polling.** Restart Correspondent without `CORRESPONDENT_NO_POLL`.
Articles FreshRSS already had are recognised by GUID (or, failing that, by
link) and are left exactly as FreshRSS served them, which is what Reeder has
cached; only new articles arrive. Nothing is deleted: retention is off unless
you set `CORRESPONDENT_RETENTION_DAYS`, and even then it only removes read,
unstarred articles that have left their feed, so they can never come back as
duplicates.

**Keep FreshRSS** stopped but intact, with its data, for a couple of weeks.

## Rolling back

Before anything was changed in Correspondent, rolling back is just pointing
the domain at FreshRSS again and restarting its refresh.

After you have read or starred things in Correspondent, replay those changes
first, so FreshRSS ends up with your current state:

```sh
# Show what would be sent:
docker compose exec correspondent /correspondent migrate rollback isma
# Send it:
docker compose exec correspondent /correspondent migrate rollback isma \
  -freshrss https://freshrss.internal.example/api/greader.php \
  -api-password-file /secrets/api_password -apply
```

Then switch the domain back. Articles that only Correspondent fetched don't
exist in FreshRSS and are skipped; FreshRSS fetches them itself under its own
IDs. Subscriptions added in Correspondent are not replayed.

## After the move

The token FreshRSS issued is static: it never expires and changes only with
the API password, which is how FreshRSS works. Once everything is settled,
retire it:

1. In Settings, set a new app password.
2. Sign in again in Reeder on each device with the new password.
3. `correspondent legacy-auth off isma`
