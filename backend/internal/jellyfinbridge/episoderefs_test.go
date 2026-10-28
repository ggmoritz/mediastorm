package jellyfinbridge

import (
	"testing"

	"novastream/config"
)

// Episode data from the metadata service updates the episode's own title
// and still but leaves the shared series details alone: continue-watching
// rows build the series from a profile's watch history.
func TestMetadataEpisodeKeepsSharedSeriesDetails(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	u := h.srv.newUserState("p1")
	stored := h.srv.seriesRefFrom(titleInfo{MediaType: "series", Name: "Show", ID: "tmdb:tv:1", ExternalIDs: map[string]string{"tmdb": "1"}, Poster: "https://img.example/poster.jpg"})
	id := h.srv.episodeItem(u, stored, episodeInfo{Season: 1, Episode: 1, Name: "TBA"}).ID

	fromHistory := stored
	fromHistory.Info.Name, fromHistory.Info.Poster = "Show (renamed by a client)", "https://img.example/other.jpg"
	h.srv.episodeItem(u, fromHistory, episodeInfo{Season: 1, Episode: 1, Name: "Pilot", Still: "https://img.example/still.jpg", FromMetadata: true})

	ref, _ := h.srv.ids.get(id)
	if ref.Ctx.Name != "Pilot" || ref.Ctx.Thumb != "https://img.example/still.jpg" {
		t.Errorf("episode title/still not updated: %+v", ref.Ctx)
	}
	if ref.Ctx.SeriesName != "Show" || ref.Ctx.Poster != "https://img.example/poster.jpg" {
		t.Errorf("series details overwritten: %+v", ref.Ctx)
	}
}
