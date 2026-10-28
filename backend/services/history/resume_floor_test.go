package history

import (
	"testing"
	"time"

	"novastream/models"
)

func TestMeetsResumeFloor(t *testing.T) {
	cases := []struct {
		name              string
		position, percent float64
		want              bool
	}{
		{"two minutes into a long film", 121, 1.1, true},
		{"exactly two minutes of a long film", 120, 1.1, false},
		{"a minute of a short episode past 5%", 70, 5.3, true},
		{"a minute of a long film", 60, 0.6, false},
		{"percent-only import past 5%", 0, 40, true},
		{"percent-only import under 5%", 0, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MeetsResumeFloor(tc.position, tc.percent); got != tc.want {
				t.Fatalf("MeetsResumeFloor(%v, %v) = %v, want %v", tc.position, tc.percent, got, tc.want)
			}
		})
	}
	if !ComputeResume(180, 10800, 0).Eligible {
		t.Fatal("three minutes into a three-hour film should be resumable")
	}
	if ComputeResume(60, 10800, 0).Eligible {
		t.Fatal("one minute into a three-hour film should not be resumable")
	}
}

func TestContinueWatchingMovieUsesResumeFloor(t *testing.T) {
	for _, tc := range []struct {
		name     string
		position float64
		want     int
	}{
		{"three minutes of a three-hour film", 180, 1},
		{"one minute of a three-hour film", 60, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := NewService(t.TempDir())
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			svc.SetMetadataService(&mockMetadataService{
				movieDetails: &models.Title{ID: "tmdb:movie:122", Name: "The Return of the King", Year: 2003, TMDBID: 122},
			})
			userID := "user-cw-movie-floor"
			if _, err := svc.UpdatePlaybackProgress(userID, models.PlaybackProgressUpdate{
				MediaType: "movie", ItemID: "tmdb:movie:122", MovieName: "The Return of the King",
				Position: tc.position, Duration: 10800,
				ExternalIDs: map[string]string{"tmdb": "122"},
			}); err != nil {
				t.Fatalf("UpdatePlaybackProgress() error = %v", err)
			}
			items, err := svc.ListContinueWatching(userID)
			if err != nil {
				t.Fatalf("ListContinueWatching() error = %v", err)
			}
			if len(items) != tc.want {
				t.Fatalf("expected %d continue watching items, got %d: %+v", tc.want, len(items), items)
			}
		})
	}
}

func TestContinueWatchingBelowFloorEpisodeNeedsSeriesHistory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		watchedE1   bool
		episode     int
		position    float64
		wantItems   int
		wantEpisode int
	}{
		{"sampled first episode", false, 1, 60, 0, 0},
		{"started first episode", false, 1, 300, 1, 1},
		{"sampled episode of a show in progress", true, 2, 60, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := NewService(t.TempDir())
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			seriesID := "tvdb:series:12345"
			userID := "user-cw-episode-floor"
			svc.SetMetadataService(&mockMetadataService{
				seriesDetails: &models.SeriesDetails{
					Title: models.Title{ID: seriesID, Name: "Test Show", TVDBID: 12345},
					Seasons: []models.SeriesSeason{{
						Number: 1,
						Episodes: []models.SeriesEpisode{
							{ID: "ep-1", Name: "Ep 1", SeasonNumber: 1, EpisodeNumber: 1, AiredDate: "2025-02-05"},
							{ID: "ep-2", Name: "Ep 2", SeasonNumber: 1, EpisodeNumber: 2, AiredDate: "2025-02-12"},
							{ID: "ep-3", Name: "Ep 3", SeasonNumber: 1, EpisodeNumber: 3, AiredDate: "2025-02-19"},
						},
					}},
				},
			})
			watchedAt := time.Now().UTC().Add(-time.Hour)
			if tc.watchedE1 {
				watched := true
				if _, err := svc.UpdateWatchHistory(userID, models.WatchHistoryUpdate{
					MediaType: "episode", ItemID: seriesID + ":s01e01", Name: "Ep 1",
					Watched: &watched, WatchedAt: watchedAt, SeriesID: seriesID,
					SeriesName: "Test Show", SeasonNumber: 1, EpisodeNumber: 1,
					ExternalIDs: map[string]string{"tvdb": "12345"},
				}); err != nil {
					t.Fatalf("UpdateWatchHistory() error = %v", err)
				}
			}
			if _, err := svc.UpdatePlaybackProgress(userID, models.PlaybackProgressUpdate{
				MediaType: "episode", ItemID: seriesID + ":s01e0" + string(rune('0'+tc.episode)),
				Position: tc.position, Duration: 2400, Timestamp: watchedAt.Add(30 * time.Minute),
				SeriesID: seriesID, SeriesName: "Test Show", SeasonNumber: 1, EpisodeNumber: tc.episode,
				ExternalIDs: map[string]string{"tvdb": "12345"},
			}); err != nil {
				t.Fatalf("UpdatePlaybackProgress() error = %v", err)
			}
			items, err := svc.ListContinueWatching(userID)
			if err != nil {
				t.Fatalf("ListContinueWatching() error = %v", err)
			}
			if len(items) != tc.wantItems {
				t.Fatalf("expected %d continue watching items, got %d: %+v", tc.wantItems, len(items), items)
			}
			if tc.wantItems == 1 && (items[0].NextEpisode == nil || items[0].NextEpisode.EpisodeNumber != tc.wantEpisode) {
				t.Fatalf("expected episode %d up next, got %+v", tc.wantEpisode, items[0].NextEpisode)
			}
		})
	}
}
