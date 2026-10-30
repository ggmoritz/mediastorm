package epg

import (
	"context"
	"log"
	"time"

	"novastream/models"
)

const (
	// exhaustedRefreshMinInterval spaces automatic refreshes so a source that only
	// ever publishes a thin window cannot make every guide query refetch it.
	exhaustedRefreshMinInterval = 30 * time.Minute
	exhaustedRefreshTimeout     = 10 * time.Minute
)

// guideCoverage summarises how much of the schedule is still useful at now.
type guideCoverage struct {
	channelsWithPrograms int
	channelsUpcoming     int // channels with a program that has not ended yet
	programs             int
	latestStop           time.Time
}

func scheduleCoverage(schedule *models.EPGSchedule, now time.Time) guideCoverage {
	var coverage guideCoverage
	if schedule == nil {
		return coverage
	}
	for _, programs := range schedule.Programs {
		if len(programs) == 0 {
			continue
		}
		coverage.channelsWithPrograms++
		coverage.programs += len(programs)
		upcoming := false
		for _, prog := range programs {
			if prog.Stop.After(coverage.latestStop) {
				coverage.latestStop = prog.Stop
			}
			if prog.Stop.After(now) {
				upcoming = true
			}
		}
		if upcoming {
			coverage.channelsUpcoming++
		}
	}
	return coverage
}

// exhausted reports whether most guide channels have run out of programs.
func (c guideCoverage) exhausted() bool {
	return c.channelsWithPrograms > 0 && c.channelsUpcoming*2 < c.channelsWithPrograms
}

func (c guideCoverage) logFields(now time.Time) (int, int, int, string, int) {
	latest := "none"
	hoursLeft := 0
	if !c.latestStop.IsZero() {
		latest = c.latestStop.UTC().Format(time.RFC3339)
		hoursLeft = int(c.latestStop.Sub(now).Hours())
	}
	return c.channelsWithPrograms, c.channelsUpcoming, c.programs, latest, hoursLeft
}

// RefreshIfExhausted starts a background refresh when the cached guide has run
// out of programs for most channels. The scheduled refresh can be 12 hours
// apart, and sources that list only the current event per channel leave the
// guide blank for most of that gap. Safe to call on every guide query.
func (s *Service) RefreshIfExhausted(reason string) {
	now := time.Now().UTC()
	s.mu.Lock()
	if s.refreshing || now.Sub(s.lastExhaustedRefresh) < exhaustedRefreshMinInterval {
		s.mu.Unlock()
		return
	}
	coverage := scheduleCoverage(s.schedule, now)
	lastUpdated := s.schedule.LastUpdated
	if !coverage.exhausted() || now.Sub(lastUpdated) < exhaustedRefreshMinInterval {
		s.mu.Unlock()
		return
	}
	s.lastExhaustedRefresh = now
	s.mu.Unlock()

	channels, upcoming, programs, latest, hoursLeft := coverage.logFields(now)
	log.Printf("[epg] guide exhausted; starting refresh reason=%q channelsWithPrograms=%d channelsUpcoming=%d programs=%d latestStop=%s hoursLeft=%d lastUpdated=%s",
		reason, channels, upcoming, programs, latest, hoursLeft, lastUpdated.UTC().Format(time.RFC3339))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), exhaustedRefreshTimeout)
		defer cancel()
		if err := s.Refresh(ctx); err != nil {
			log.Printf("[epg] exhausted-guide refresh failed reason=%q: %v", reason, err)
		}
	}()
}
