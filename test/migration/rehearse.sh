#!/usr/bin/env bash
# Rehearses the FreshRSS → Correspondent cutover end to end, against a real
# FreshRSS and Correspondent's own image, and fails on any broken invariant:
#
#   1. Reeder's existing token keeps working; nothing needs reconfiguring.
#   2. Feeds, folders, item IDs, timestamps and read/star state match.
#   3. Polling after cutover creates no duplicates and changes no state.
#   4. Changes made in Correspondent replay into FreshRSS (rollback).
#   5. New articles get IDs above everything imported and reach Reeder's
#      incremental sync.
#
# Usage: test/migration/rehearse.sh   (needs Docker, curl, jq; run `make build` first)
set -euo pipefail
cd "$(dirname "$0")"

BIN=../../bin/correspondent
FR=http://localhost:8091/api/greader.php
CO=http://localhost:8092/api/greader.php
WEB_PASSWORD=$(openssl rand -hex 12)
API_PASSWORD=$(openssl rand -hex 12)
export WEB_PASSWORD API_PASSWORD

step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
ok() { printf '   ✓ %s\n' "$*"; }
fail() { printf '   ✗ %s\n' "$*" >&2; exit 1; }
cleanup() { [[ ${KEEP:-} == 1 ]] || docker compose down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

login() { curl -sf --data-urlencode Email=reader --data-urlencode "Passwd=$API_PASSWORD" "$1/accounts/ClientLogin" | sed -n 's/^Auth=//p'; }
reader() { # reader BASE AUTH PATH [curl args...]
  local base=$1 auth=$2 path=$3; shift 3
  curl -sf -H "Authorization: GoogleLogin auth=$auth" "$base$path" "$@"
}
ids() { # ids BASE AUTH STREAM [EXCLUDE]
  local q="/reader/api/0/stream/items/ids?output=json&n=10000&s=$3"
  [[ -n ${4:-} ]] && q+="&xt=$4"
  reader "$1" "$2" "$q" | jq -r '.itemRefs[].id' | sort
}
edit() { # edit BASE AUTH T a|r TAG ID...
  local base=$1 auth=$2 t=$3 op=$4 tag=$5; shift 5
  local args=(-d "T=$t" -d "$op=$tag")
  for i in "$@"; do args+=(-d "i=$i"); done
  [[ $(reader "$base" "$auth" /reader/api/0/edit-tag "${args[@]}") == OK ]] || fail "edit-tag $op $tag"
}
wait_for() { # wait_for DESCRIPTION COMMAND...
  local desc=$1; shift
  for _ in $(seq 1 90); do "$@" >/dev/null 2>&1 && return 0; sleep 1; done
  fail "timed out waiting for $desc"
}
verify() {
  "$BIN" migrate verify reader -freshrss "$FR" -correspondent "$CO" \
    -api-password-file work/api_password -data work/verify-db | sed 's/^/   /'
}
READ=user/-/state/com.google/read
STAR=user/-/state/com.google/starred
ALL=user/-/state/com.google/reading-list

[[ -x $BIN ]] || fail "build first: make build"
rm -rf work && mkdir -p work/fixtures && cp fixtures/* work/fixtures/
printf '%s' "$API_PASSWORD" >work/api_password

step "Start FreshRSS 1.30.4 and fixture feeds"
docker compose build -q correspondent
docker compose up -d --wait fixtures freshrss >/dev/null 2>&1 || docker compose up -d fixtures freshrss
wait_for "FreshRSS API" login "$FR"
# FreshRSS refuses to fetch private addresses (as Correspondent does); allow
# only the Docker network the fixture server lives on. Not needed in production.
docker compose exec -T -u www-data freshrss php -r '$f = "/var/www/FreshRSS/data/config.php"; $c = include $f;
  $c["internal_host_allowlist"] = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"];
  file_put_contents($f, "<?php\nreturn " . var_export($c, true) . ";\n");'
docker compose cp work/fixtures/feeds.opml freshrss:/tmp/feeds.opml >/dev/null
docker compose exec -T freshrss chmod 644 /tmp/feeds.opml
docker compose exec -T -u www-data freshrss php cli/import-for-user.php --user reader --filename /tmp/feeds.opml >/dev/null
docker compose exec -T -u www-data freshrss php cli/actualize-user.php --user reader >/dev/null
REEDER=$(login "$FR") # the token Reeder holds; it must keep working after the switch
ALL_IDS=($(ids "$FR" "$REEDER" "$ALL"))
(( ${#ALL_IDS[@]} == 22 )) || fail "FreshRSS fetched ${#ALL_IDS[@]} items, expected 22"
ok "FreshRSS fetched 22 items from 4 feeds in 2 folders"

T=$(reader "$FR" "$REEDER" /reader/api/0/token)
edit "$FR" "$REEDER" "$T" a "$READ" "${ALL_IDS[@]:0:10}"
edit "$FR" "$REEDER" "$T" a "$STAR" "${ALL_IDS[2]}" "${ALL_IDS[15]}" "${ALL_IDS[20]}"
ok "reading state set through the API: 10 read, 3 starred"

step "Export from FreshRSS"
docker compose exec -T -u www-data freshrss php cli/export-sqlite-for-user.php --user reader --filename /tmp/reader.sqlite >/dev/null
docker compose cp freshrss:/tmp/reader.sqlite work/reader.sqlite >/dev/null
docker compose cp freshrss:/var/www/FreshRSS/data/config.php work/config.php >/dev/null
docker compose cp freshrss:/var/www/FreshRSS/data/users/reader/config.php work/user-config.php >/dev/null
chmod 644 work/*.sqlite work/*.php work/api_password
ok "official SQLite export plus configuration"

step "Import into Correspondent (polling off)"
docker compose run --rm --no-deps -T correspondent migrate import reader \
  -export /work/reader.sqlite -system-config /work/config.php -user-config /work/user-config.php | sed 's/^/   /'
NO_POLL=1 docker compose up -d correspondent >/dev/null 2>&1
wait_for "Correspondent" curl -sf http://localhost:8092/ready

step "Verify: what Reeder sees must be identical"
verify || fail "verification failed after import"

step "Polling on: refetching every feed must change nothing"
BEFORE=($(ids "$CO" "$REEDER" "$ALL"))
NO_POLL=0 docker compose up -d correspondent >/dev/null 2>&1
wait_for "Correspondent" curl -sf http://localhost:8092/ready
polled() { (( $(docker compose logs correspondent 2>&1 | grep -c '"msg":"poll"') >= 4 )); }
wait_for "first poll of all feeds" polled
AFTER=($(ids "$CO" "$REEDER" "$ALL"))
[[ "${BEFORE[*]}" == "${AFTER[*]}" ]] || fail "polling changed the item set: ${#BEFORE[@]} → ${#AFTER[@]} (duplicates)"
ok "all 4 feeds polled; still exactly ${#AFTER[@]} items, no duplicates"
verify || fail "verification failed after polling"

step "Changes made in Correspondent replay into FreshRSS (rollback)"
UNREAD=($(ids "$CO" "$REEDER" "$ALL" "$READ"))
STARRED=($(ids "$CO" "$REEDER" "$STAR"))
edit "$CO" "$REEDER" x a "$READ" "${UNREAD[0]}" "${UNREAD[1]}"   # Reeder sends T=x
edit "$CO" "$REEDER" x r "$READ" "${ALL_IDS[0]}"
edit "$CO" "$REEDER" x a "$STAR" "${UNREAD[2]}"
edit "$CO" "$REEDER" x r "$STAR" "${STARRED[0]}"
ok "with Reeder's old token: 2 read, 1 unread, 1 starred, 1 unstarred"
docker compose exec -T correspondent /correspondent migrate rollback reader \
  -freshrss http://freshrss/api/greader.php -api-password-file /work/api_password -apply | sed 's/^/   /'
verify || fail "FreshRSS and Correspondent disagree after rollback replay"

step "A new article after cutover reaches Reeder's incremental sync"
CUTOVER=$(date +%s)
sleep 1
cp work/fixtures/changes-v2.xml work/fixtures/changes.xml
JAR=work/cookies
curl -sf -c "$JAR" -H 'X-Correspondent: 1' -H 'Content-Type: application/json' \
  -d "{\"username\":\"reader\",\"password\":\"$WEB_PASSWORD\"}" http://localhost:8092/api/v1/session >/dev/null ||
  fail "the FreshRSS web password does not sign in to Correspondent"
ok "FreshRSS web password signs in to Correspondent"
FEED=$(reader "$CO" "$REEDER" '/reader/api/0/subscription/list?output=json' | jq -r '.subscriptions[] | select(.url | endswith("changes.xml")) | .id | ltrimstr("feed/")')
curl -sf -b "$JAR" -X POST -H 'X-Correspondent: 1' "http://localhost:8092/api/v1/feeds/$FEED/refresh" >/dev/null
NEW=($(reader "$CO" "$REEDER" "/reader/api/0/stream/items/ids?output=json&n=100&s=$ALL&ot=$CUTOVER" | jq -r '.itemRefs[].id'))
(( ${#NEW[@]} == 1 )) || fail "expected exactly 1 new item since cutover, got ${#NEW[@]}"
MAX_OLD=$(printf '%s\n' "${ALL_IDS[@]}" | sort -n | tail -1)
(( NEW[0] > MAX_OLD )) || fail "new item ID ${NEW[0]} is not above the imported maximum $MAX_OLD"
TITLE=$(reader "$CO" "$REEDER" '/reader/api/0/stream/items/contents?output=json' -d "i=${NEW[0]}" | jq -r '.items[0].title')
[[ $TITLE == "Change log 4, published after cutover" ]] || fail "unexpected new item: $TITLE"
ok "1 new item, ID ${NEW[0]} > $MAX_OLD, found by ot=$CUTOVER"

printf '\n\033[1mRehearsal passed.\033[0m\n'
