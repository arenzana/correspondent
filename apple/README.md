# Correspondent for iPhone, iPad and Mac

A native SwiftUI app with the web app's design: the same typefaces
(Newsreader, Schibsted Grotesk), themes (Day, Evening, Night desk) and time
spine. iPad and Mac show three columns; iPhone uses iOS's tab bar and
navigation, with the web's list and article inside.

It talks to a Correspondent server through `/api/v1` with a personal token
(made at sign-in from the username and password, kept in the Keychain), and
keeps unread and starred articles on the device for reading offline.

```sh
cd apple
xcodegen generate                     # Correspondent.xcodeproj (not in git)
(cd Kit && swift test)                # the API client, events, offline copy
open Correspondent.xcodeproj
```

| Path | |
|---|---|
| `Kit/` | `CorrespondentKit`: models, client, live events, offline store, Keychain |
| `App/Sources/` | the app: state (`AppModel`), theme and type, views |
| `App/Resources/Fonts/` | the web's typefaces as TrueType (SIL Open Font License) |

Debug builds can start signed in for tests and screenshots:
`CORR_SERVER=http://127.0.0.1:8099 CORR_TOKEN=corr_… CORR_OPEN_FIRST=1`.

The Mac app is distributed as a notarized download attached to each GitHub
release (`scripts/release-mac.sh VERSION`); iPhone and iPad through
TestFlight and the App Store. The script needs Developer ID signing: the
App Store Connect key with access to cloud-managed Developer ID
certificates, or a Developer ID Application certificate in the keychain.
