package content

import "testing"

func TestSponsored(t *testing.T) {
	for _, c := range []struct {
		title string
		cats  []string
		want  bool
	}{
		{"Sponsor: Audio Hijack, from Rogue Amoeba: Record any audio on your Mac", nil, true},
		{"[Sponsor] Kolide", nil, true},
		{"  sponsored: a thing", nil, true},
		{"Sponsored by Linode: cloud for developers", nil, true},
		{"(Sponsor) Something", nil, true},
		{"Paid post: The future", nil, true},
		{"A plain post", []string{"Apple", "Sponsor"}, true},
		{"A plain post", []string{" Sponsored Content "}, true},
		{"Sponsors and their discontents", nil, false},
		{"Why I sponsor open source: a guide", nil, false},
		{"The sponsor list for WWDC", []string{"Apple"}, false},
		{"Advertisement in the 1950s", nil, false},
	} {
		if got := Sponsored(c.title, c.cats); got != c.want {
			t.Errorf("Sponsored(%q, %v) = %v, want %v", c.title, c.cats, got, c.want)
		}
	}
}
