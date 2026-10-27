#!/bin/bash
# Archives the apps and uploads them to App Store Connect for TestFlight:
#
#   apple/scripts/release-testflight.sh [ios|macos|all]
#
# Signs automatically through the Apple ID signed in to Xcode (Settings >
# Accounts), which registers identifiers, the App Group and profiles as
# needed. The version is 1.0 (the App Store version); the build number is
# the time, so every upload is new.
set -euo pipefail
# Xcode's export copies with the system rsync; Homebrew's breaks it ("Copy failed").
export PATH=/usr/bin:/bin:/usr/sbin:/sbin:$PATH

which=${1:-all}
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
build=$(date -u +%Y%m%d%H%M)

cat > "$work/export.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>app-store-connect</string>
  <key>destination</key><string>upload</string>
  <key>signingStyle</key><string>automatic</string>
  <key>teamID</key><string>76L847LW9X</string>
  <key>manageAppVersionAndBuildNumber</key><false/>
</dict></plist>
PLIST

xcodegen generate >/dev/null

upload() {
  local platform=$1 destination=$2
  echo "== $platform $build"
  xcodebuild archive -project Correspondent.xcodeproj -scheme Correspondent -destination "$destination" \
    -archivePath "$work/$platform.xcarchive" -allowProvisioningUpdates \
    MARKETING_VERSION=1.0 CURRENT_PROJECT_VERSION="$build" | tail -2
  xcodebuild -exportArchive -archivePath "$work/$platform.xcarchive" -exportPath "$work/$platform" \
    -exportOptionsPlist "$work/export.plist" -allowProvisioningUpdates | tail -3
}

case $which in
  ios) upload ios 'generic/platform=iOS' ;;
  macos) upload macos 'generic/platform=macOS' ;;
  all) upload ios 'generic/platform=iOS'; upload macos 'generic/platform=macOS' ;;
  *) echo "usage: release-testflight.sh [ios|macos|all]" >&2; exit 2 ;;
esac
echo "Uploaded build $build. It appears in TestFlight once App Store Connect has processed it."
