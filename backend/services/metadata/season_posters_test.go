package metadata

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"novastream/models"
)

const malcolmSeasonBanner = "https://artworks.thetvdb.com/banners/v4/season/6527/banners/64ddac2483fe8.jpg"

func TestSeriesDetailsRepairsCachedSeasonBanners(t *testing.T) {
	for _, lite := range []bool{false, true} {
		t.Run(fmt.Sprintf("lite=%v", lite), func(t *testing.T) {
			cache := newFileCache(t.TempDir(), 24)
			var posterCalls atomic.Int32
			httpc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{}`
				if r.URL.Path == "/3/tv/2004/season/1" {
					posterCalls.Add(1)
					body = `{"id":6527,"season_number":1,"poster_path":"/season-one.jpg","episodes":[]}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			s := &Service{client: newTVDBClient("tvdb-key", "eng", httpc, 24), tmdb: newTMDBClient("tmdb-key", "eng", httpc, cache), cache: cache, idCache: newFileCache(t.TempDir(), 168), inflightRequests: make(map[string]*inflightRequest)}
			s.tmdb.minInterval = 0
			details := models.SeriesDetails{
				Title:               models.Title{ID: "tvdb:series:73838", Name: "Malcolm in the Middle", TVDBID: 73838, TMDBID: 2004, IMDBID: "tt0212671", Backdrop: &models.Image{URL: "backdrop"}, Logo: &models.Image{URL: "logo", Width: 10, Height: 10}, Credits: &models.Credits{}, Genres: []string{"Comedy"}, Certification: "TV-PG"},
				EpisodeTMDBEnriched: true, ActiveOrdering: "official",
				Seasons: []models.SeriesSeason{
					{Number: 1, Name: "Season 1", TVDBID: 6527, Image: &models.Image{URL: malcolmSeasonBanner, Type: "poster"}, Episodes: []models.SeriesEpisode{{ID: "tvdb:episode:1", SeasonNumber: 1, EpisodeNumber: 1}}},
					{Number: 2, Image: &models.Image{URL: "https://artworks.thetvdb.com/banners/seasons/73838-2.jpg", Type: "poster"}},
				},
			}
			if err := cache.set(seriesDetailsCacheKey("eng", 73838, ""), details); err != nil {
				t.Fatal(err)
			}
			fetch := s.SeriesDetails
			if lite {
				fetch = s.SeriesDetailsLite
			}
			for i := 0; i < 2; i++ {
				got, err := fetch(t.Context(), models.SeriesDetailsQuery{TVDBID: 73838, TMDBID: 2004, IMDBID: "tt0212671"})
				if err != nil {
					t.Fatal(err)
				}
				if image := got.Seasons[0].Image; image == nil || !strings.HasSuffix(image.URL, "/season-one.jpg") || !image.IsPortraitPoster() {
					t.Fatalf("cached banner was not replaced by a season poster: %+v", image)
				}
				if got.Seasons[1].Image.URL != details.Seasons[1].Image.URL {
					t.Fatal("existing portrait season poster was replaced")
				}
				if got.Seasons[0].Episodes[0].ID != "tvdb:episode:1" || got.Seasons[0].Episodes[0].EpisodeNumber != 1 {
					t.Fatal("poster repair changed episode identity or numbering")
				}
			}
			if got := posterCalls.Load(); got != 1 {
				t.Fatalf("TMDB season fetched %d times, want one cached fetch", got)
			}
		})
	}
}

func TestSeasonBannerIsRemovedWithoutMatchingPosterProvider(t *testing.T) {
	for _, ordering := range []string{"official", "dvd", "absolute"} {
		t.Run(ordering, func(t *testing.T) {
			details := models.SeriesDetails{ActiveOrdering: ordering, Seasons: []models.SeriesSeason{{Number: 1, Image: &models.Image{URL: malcolmSeasonBanner, Type: "poster"}}}}
			s := &Service{}
			if ordering != "official" {
				httpc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					t.Errorf("alternate ordering requested a TMDB season: %s", r.URL.Path)
					return nil, fmt.Errorf("unexpected request")
				})}
				s.tmdb = newTMDBClient("key", "eng", httpc, nil)
			}
			s.repairSeasonPosters(t.Context(), &details, models.SeriesDetailsQuery{TMDBID: 2004})
			if details.Seasons[0].Image != nil {
				t.Fatal("wide season banner must not be returned as a poster")
			}
		})
	}
}
