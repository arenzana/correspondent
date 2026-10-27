#!/bin/bash
# Archives the apps and uploads them to App Store Connect for TestFlight:
#
#   apple/scripts/release-testflight.sh [ios|macos|all]
#
# Signs with the Apple Distribution and Mac installer certificates in the
# keychain and the "Correspondent … App Store" profiles (made through the App
# Store Connect API, so nothing depends on an Apple ID signed in to Xcode,
# whose session lapses), and uploads with the API key (1Password: infra /
# App Store Connect - Admin).
# The version is 1.0 (the App Store version); the build number is the time,
# so every upload is new.
set -euo pipefail
# Xcode's export copies with the system rsync; Homebrew's breaks it ("Copy failed").
export PATH=/usr/bin:/bin:/usr/sbin:/sbin:$PATH

which=${1:-all}
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

# altool finds the key as AuthKey_<ID>.p8 in API_PRIVATE_KEYS_DIR.
op=${OP:-$HOME/.local/bin/op-sa}
key_id=8ZJB9WFM9J
mkdir -m 700 "$work/keys"
"$op" read "op://infra/App Store Connect - Admin/AuthKey_$key_id.p8" > "$work/keys/AuthKey_$key_id.p8"
issuer=$("$op" read "op://infra/App Store Connect - Admin/Issuer ID" | tr -d '\n')
export API_PRIVATE_KEYS_DIR="$work/keys"
build=$(date -u +%Y%m%d%H%M)

# Manual signing for the App Store, per platform.
export_plist() {
  local profiles installer=""
  if [ "$1" = ios ]; then
    profiles='<key>org.arenzana.correspondent</key><string>Correspondent App Store</string>
      <key>org.arenzana.correspondent.share</key><string>Correspondent Share App Store</string>
      <key>org.arenzana.correspondent.widgets</key><string>Correspondent Widgets App Store</string>'
  else
    profiles='<key>org.arenzana.correspondent</key><string>Correspondent Mac App Store</string>'
    installer='<key>installerSigningCertificate</key><string>3rd Party Mac Developer Installer</string>'
  fi
  cat > "$work/export-$1.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>app-store-connect</string>
  <key>destination</key><string>export</string>
  <key>signingStyle</key><string>manual</string>
  <key>signingCertificate</key><string>Apple Distribution</string>
  $installer
  <key>provisioningProfiles</key><dict>
      $profiles
  </dict>
  <key>teamID</key><string>76L847LW9X</string>
  <key>manageAppVersionAndBuildNumber</key><false/>
</dict></plist>
PLIST
}

xcodegen generate >/dev/null

upload() {
  local platform=$1 destination=$2
  echo "== $platform $build"
  xcodebuild archive -project Correspondent.xcodeproj -scheme Correspondent -destination "$destination" \
    -archivePath "$work/$platform.xcarchive" -allowProvisioningUpdates \
    MARKETING_VERSION=1.0 CURRENT_PROJECT_VERSION="$build" | tail -2
  export_plist "$platform"
  xcodebuild -exportArchive -archivePath "$work/$platform.xcarchive" -exportPath "$work/$platform" \
    -exportOptionsPlist "$work/export-$platform.plist" | grep -E "error|EXPORT" || true
  # iOS exports an .ipa, macOS a .pkg.
  local package
  package=$(find "$work/$platform" -maxdepth 1 \( -name '*.ipa' -o -name '*.pkg' \) | head -1)
  [ -n "$package" ] || { echo "nothing was exported for $platform" >&2; exit 1; }
  xcrun altool --upload-app -f "$package" -t "$platform" --apiKey "$key_id" --apiIssuer "$issuer" 2>&1 | tail -2
}

case $which in
  ios) upload ios 'generic/platform=iOS' ;;
  macos) upload macos 'generic/platform=macOS' ;;
  all) upload ios 'generic/platform=iOS'; upload macos 'generic/platform=macOS' ;;
  *) echo "usage: release-testflight.sh [ios|macos|all]" >&2; exit 2 ;;
esac
echo "Uploaded build $build. It appears in TestFlight once App Store Connect has processed it."
