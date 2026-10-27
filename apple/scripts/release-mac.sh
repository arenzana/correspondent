#!/bin/bash
# Builds the Mac app signed with Developer ID, notarizes and staples it, and
# attaches it to the GitHub release:
#
#   apple/scripts/release-mac.sh 0.18.0
#
# Needs the App Store Connect API key (1Password: infra / App Store Connect -
# Admin) with access to Developer ID signing, or a Developer ID Application
# certificate in the keychain. The key is copied to a private temporary
# folder for the build and deleted afterwards.
set -euo pipefail

version=${1:?usage: release-mac.sh VERSION}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a version: $version" >&2; exit 2; }
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
chmod 700 "$work"

op=${OP:-$HOME/.local/bin/op-sa}
"$op" read "op://infra/App Store Connect - Admin/AuthKey_8ZJB9WFM9J.p8" > "$work/key.p8"
issuer=$("$op" read "op://infra/App Store Connect - Admin/Issuer ID" | tr -d '\n')
auth=(-allowProvisioningUpdates -authenticationKeyPath "$work/key.p8" -authenticationKeyID 8ZJB9WFM9J -authenticationKeyIssuerID "$issuer")

xcodegen generate >/dev/null
xcodebuild archive -project Correspondent.xcodeproj -scheme Correspondent -destination 'generic/platform=macOS' \
  -archivePath "$work/Correspondent.xcarchive" MARKETING_VERSION="$version" "${auth[@]}" | tail -2

cat > "$work/export.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>method</key><string>developer-id</string>
  <key>signingStyle</key><string>automatic</string>
  <key>teamID</key><string>76L847LW9X</string>
</dict></plist>
PLIST
xcodebuild -exportArchive -archivePath "$work/Correspondent.xcarchive" -exportPath "$work/export" \
  -exportOptionsPlist "$work/export.plist" "${auth[@]}" | tail -2

app="$work/export/Correspondent.app"
zip="$work/Correspondent-$version-mac.zip"
ditto -c -k --keepParent "$app" "$zip"
xcrun notarytool submit "$zip" --key "$work/key.p8" --key-id 8ZJB9WFM9J --issuer "$issuer" --wait
xcrun stapler staple "$app"
rm "$zip"
ditto -c -k --keepParent "$app" "$zip"
spctl --assess --type execute --verbose "$app"

gh release upload "v$version" "$zip" --clobber -R arenzana/correspondent
echo "Attached Correspondent-$version-mac.zip to v$version."
