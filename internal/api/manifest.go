package api

import (
	"encoding/json"
	"net/http"
)

// appIcons are the icon sets shipped in the UI (ui/public/icons/<name>/).
var appIcons = map[string]bool{"wire": true, "byline": true, "morse": true}

// Manifest serves the web app manifest with the signed-in user's chosen
// icon, so an installed app matches the icon picked in Settings. Signed-out
// requests get the default icon.
func (a *API) Manifest(w http.ResponseWriter, r *http.Request) {
	icon := "wire"
	if c, err := r.Cookie(cookieName); err == nil {
		if uid, err := a.Store.SessionUser(r.Context(), c.Value, "web"); err == nil {
			if u, err := a.Store.UserByID(r.Context(), uid); err == nil {
				var prefs struct {
					AppIcon string `json:"appIcon"`
				}
				if json.Unmarshal([]byte(u.Prefs), &prefs) == nil && appIcons[prefs.AppIcon] {
					icon = prefs.AppIcon
				}
			}
		}
	}
	base := "/icons/" + icon + "/"
	manifest := map[string]any{
		"name":             "Correspondent",
		"short_name":       "Correspondent",
		"id":               "/",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"background_color": "#EEF1F5",
		"theme_color":      "#2742C7",
		"icons": []map[string]string{
			{"src": base + "icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
			{"src": base + "icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
			{"src": base + "icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "Cookie")
	json.NewEncoder(w).Encode(manifest)
}
