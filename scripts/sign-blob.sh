#!/bin/sh
# sign-blob.sh BUNDLE FILE signs FILE with cosign, keyless, writing the
# Sigstore bundle to BUNDLE. Rekor sometimes answers slowly and cosign's
# own retry then collides with its first attempt ("409 an equivalent
# entry already exists"); signing again from the start uses a new key, so
# each try records a fresh entry. (A script, not sh -c in .goreleaser.yaml:
# goreleaser expands $variables in a command's arguments.)
set -u
for try in 1 2 3; do
  cosign sign-blob --bundle="$1" "$2" --yes && exit 0
  echo "signing failed (try $try); trying again" >&2
  sleep $((try * 15))
done
exit 1
