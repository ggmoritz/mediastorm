package metadata

import (
	"context"
	"log"
	"strings"
	"sync"

	"novastream/models"
)

// repairSeasonPosters also runs on cached details: TVDB season.image may be a
// banner (e.g. Malcolm's 400x73 season headers), not a portrait season cover.
// Reuse the TMDB season cache to replace it without changing episode ordering.
func (s *Service) repairSeasonPosters(ctx context.Context, details *models.SeriesDetails, req models.SeriesDetailsQuery) {
	if details == nil {
		return
	}
	var invalid []int
	for i := range details.Seasons {
		image := details.Seasons[i].Image
		if image != nil && !image.IsPortraitPoster() {
			details.Seasons[i].Image = nil
			if details.Seasons[i].Number >= 0 {
				invalid = append(invalid, i)
			}
		}
	}
	if len(invalid) == 0 || s.tmdb == nil || !s.tmdb.isConfigured() {
		return
	}
	ordering := strings.ToLower(strings.TrimSpace(firstNonEmpty(details.ActiveOrdering, req.SeasonType)))
	if ordering != "" && ordering != "default" && ordering != "official" {
		// TMDB's aired seasons do not identify DVD/absolute/alternate seasons.
		return
	}
	tmdbID := details.Title.TMDBID
	if tmdbID <= 0 {
		tmdbID = req.TMDBID
	}
	if tmdbID <= 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for _, index := range invalid {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			season := &details.Seasons[index]
			tmdbSeason, found, err := s.cachedTMDBSeason(ctx, tmdbID, tmdbSeasonSummary{Number: season.Number})
			if err != nil {
				log.Printf("[metadata] season poster fallback failed tmdbId=%d season=%d: %v", tmdbID, season.Number, err)
				return
			}
			if found && tmdbSeason.Image.IsPortraitPoster() {
				image := *tmdbSeason.Image
				season.Image = &image
			}
		}(index)
	}
	wg.Wait()
}
