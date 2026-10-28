package jellyfinbridge

import (
	"testing"

	"novastream/config"
)

// An episode registered before it aired ("TBA") gets its real title later,
// whether the title comes from metadata or from another listing.
func TestEpisodeTitleReplacesPlaceholder(t *testing.T) {
	s := newIDStore("")
	src := "tmdb:tv:105248:s02e01"
	id := s.put(refEpisode, src, refContext{Name: "TBA", Season: 2, Episode: 1})

	s.put(refEpisode, src, refContext{Name: "Last Chance for a Slow Dance"})
	if ref, _ := s.get(id); ref.Ctx.Name != "Last Chance for a Slow Dance" {
		t.Fatalf("placeholder kept: %q", ref.Ctx.Name)
	}
	// A sparse or user-supplied record still only fills gaps.
	s.put(refEpisode, src, refContext{Name: "Something else"})
	if ref, _ := s.get(id); ref.Ctx.Name != "Last Chance for a Slow Dance" {
		t.Fatalf("fill-only put replaced a real title: %q", ref.Ctx.Name)
	}
	// Metadata replaces it.
	s.putTrusted(refEpisode, src, refContext{Name: "Last Chance for a Slow Dance (Part 1)"})
	if ref, _ := s.get(id); ref.Ctx.Name != "Last Chance for a Slow Dance (Part 1)" || ref.Ctx.Season != 2 {
		t.Fatalf("trusted put: %+v", ref.Ctx)
	}
	// Stale continue-watching data still saying "TBA" does not undo it.
	s.putTrusted(refEpisode, src, refContext{Name: "TBA"})
	if ref, _ := s.get(id); ref.Ctx.Name != "Last Chance for a Slow Dance (Part 1)" {
		t.Fatalf("placeholder replaced a real title: %q", ref.Ctx.Name)
	}
}

// A progress row written before the episode had a name ("TBA") shows the
// stored title.
func TestEpisodeItemPrefersStoredTitleOverPlaceholder(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	series := h.srv.seriesRefFrom(titleInfo{MediaType: "series", Name: "Cyberpunk: Edgerunners", ID: "tmdb:tv:105248", ExternalIDs: map[string]string{"tmdb": "105248"}})
	u := h.srv.newUserState("p1")
	h.srv.episodeItem(u, series, episodeInfo{Season: 2, Episode: 8, Name: "Serce", FromMetadata: true})
	if item := h.srv.episodeItem(u, series, episodeInfo{Season: 2, Episode: 8, Name: "TBA"}); item.Name != "Serce" {
		t.Fatalf("name = %q", item.Name)
	}
}
