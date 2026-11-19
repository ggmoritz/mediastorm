package jellyfinbridge

import (
	"context"
	"testing"

	"novastream/config"
	"novastream/models"
)

// Playing a preset version drops what Auto cached for that device (mediastorm
// now ranks the title there with that preset), and nothing of other devices,
// other profiles or preset versions.
func TestForgetAutoRanking(t *testing.T) {
	s := &Server{releases: newReleaseCache(), autoPicks: newAutoPicks()}
	s.presetChoices.items = map[string]itemPresets{"p1|d1|ep2": {}, "p1|d2|ep2": {}, "p2|d1|ep2": {}}
	for _, key := range []string{"p1|d1|ep2|", "p1|d2|ep3|", "p1|d1|ep2|preset-a", "p2|d1|ep2|"} {
		s.autoPicks.byKey[key] = &autoPick{}
		s.releases.lists[key] = &releaseList{}
	}

	s.forgetAutoRanking("p1|d1")

	if _, ok := s.presetChoices.items["p1|d1|ep2"]; ok {
		t.Error("Auto's label for the device is still cached")
	}
	if _, ok := s.presetChoices.items["p1|d2|ep2"]; !ok {
		t.Error("another device's label was dropped")
	}
	if _, ok := s.presetChoices.items["p2|d1|ep2"]; !ok {
		t.Error("another profile's label was dropped")
	}
	for key, want := range map[string]bool{"p1|d1|ep2|": false, "p1|d2|ep3|": true, "p1|d1|ep2|preset-a": true, "p2|d1|ep2|": true} {
		if _, ok := s.autoPicks.byKey[key]; ok != want {
			t.Errorf("auto pick %q kept = %v, want %v", key, ok, want)
		}
		if _, ok := s.releases.lists[key]; ok != want {
			t.Errorf("release list %q kept = %v, want %v", key, ok, want)
		}
	}
}

// The preset version the item gets without a remembered preset is marked.
func TestPresetVersionLabelMarksDefault(t *testing.T) {
	german := presetChoice{ID: "g1", Name: "German HD"}
	simple := presetChoice{ID: "simple", Name: "Simple ranking"}
	cases := []struct {
		c    presetChoice
		def  presetSelection
		want string
	}{
		{german, presetSelection{PresetID: "g1", Source: "profile"}, "Preset · German HD (default)"},
		{german, presetSelection{PresetID: "other", Source: "profile"}, "Preset · German HD"},
		{simple, presetSelection{Source: "none"}, "Preset · Simple ranking (default)"},
		{german, presetSelection{}, "Preset · German HD"}, // default unknown
		{simple, presetSelection{}, "Preset · Simple ranking"},
	}
	for _, tc := range cases {
		if got := presetVersionLabel(tc.c, tc.def); got != tc.want {
			t.Errorf("%+v with default %+v: %q, want %q", tc.c, tc.def, got, tc.want)
		}
	}
}

// tvdbSeriesMetadata knows a series by its TVDB ID and reports its TMDB ID,
// as the metadata service does.
type tvdbSeriesMetadata struct{ fakeMetadata }

func (tvdbSeriesMetadata) SeriesInfo(_ context.Context, q models.SeriesDetailsQuery) (*models.Title, error) {
	return &models.Title{ID: q.TitleID, Name: "The End of the F***ing World", MediaType: "series", TMDBID: 74577, TVDBID: 336522, IMDBID: "tt6257970"}, nil
}

// The bridge sends mediastorm the canonical title ID (TMDB for a series it
// knows by its TVDB ID), the form the prequeue stores remembered presets
// under: the preset list, searches and playback then name the same title.
func TestPrequeueRequestUsesCanonicalTitleID(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	h.srv.deps.Metadata = tvdbSeriesMetadata{}
	p := &principal{}
	p.Profile.ID = "u1"
	req, err := h.srv.prequeueRequest(context.Background(), p, itemRef{Kind: refEpisode, Source: "tvdb:series:336522:s1e4"})
	if err != nil {
		t.Fatal(err)
	}
	if req.TitleID != "tmdb:tv:74577" || req.TmdbID != "74577" || req.TvdbID != "336522" {
		t.Errorf("request = titleId %q tmdb %q tvdb %q", req.TitleID, req.TmdbID, req.TvdbID)
	}
}
