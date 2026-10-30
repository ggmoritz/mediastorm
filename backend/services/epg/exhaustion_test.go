package epg

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"novastream/config"
	"novastream/models"
)

func endedSchedule(now time.Time, channels int, lastUpdated time.Time) *models.EPGSchedule {
	schedule := &models.EPGSchedule{
		Channels:    map[string]models.EPGChannel{},
		Programs:    map[string][]models.EPGProgram{},
		LastUpdated: lastUpdated,
	}
	for i := 0; i < channels; i++ {
		id := fmt.Sprintf("game-%d", i)
		schedule.Channels[id] = models.EPGChannel{ID: id, Name: id}
		schedule.Programs[id] = []models.EPGProgram{{
			ChannelID: id,
			Title:     "Game",
			Start:     now.Add(-5 * time.Hour),
			Stop:      now.Add(-2 * time.Hour),
		}}
	}
	return schedule
}

func TestScheduleCoverageExhausted(t *testing.T) {
	now := time.Now().UTC()
	schedule := endedSchedule(now, 4, now.Add(-time.Hour))
	if !scheduleCoverage(schedule, now).exhausted() {
		t.Fatal("guide where every program has ended should be exhausted")
	}

	// Half the channels still airing is not exhausted.
	for _, id := range []string{"game-0", "game-1"} {
		schedule.Programs[id] = append(schedule.Programs[id], models.EPGProgram{
			ChannelID: id, Title: "Late Game", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour),
		})
	}
	coverage := scheduleCoverage(schedule, now)
	if coverage.exhausted() {
		t.Fatalf("coverage %+v should not be exhausted", coverage)
	}
	if coverage.channelsUpcoming != 2 || coverage.programs != 6 || !coverage.latestStop.Equal(now.Add(time.Hour)) {
		t.Fatalf("unexpected coverage %+v", coverage)
	}

	if scheduleCoverage(&models.EPGSchedule{}, now).exhausted() {
		t.Fatal("empty guide must not count as exhausted")
	}
}

func TestRefreshIfExhaustedRefreshesAndThrottles(t *testing.T) {
	now := time.Now().UTC()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<tv>
  <channel id="game-0"><display-name>Game 0</display-name></channel>
  <programme start="%s" stop="%s" channel="game-0"><title>Tonight</title></programme>
</tv>`, now.Add(-time.Hour).Format("20060102150405 -0700"), now.Add(2*time.Hour).Format("20060102150405 -0700"))
	}))
	defer server.Close()

	settings := config.DefaultSettings()
	settings.Cache.Directory = t.TempDir()
	settings.Live.EPG.Enabled = true
	settings.Live.EPG.XmltvUrl = server.URL + "/guide.xml"
	settings.Live.Sources = nil
	settings.Live.PlaylistSources = nil
	manager := config.NewManager(filepath.Join(t.TempDir(), "settings.json"))
	if err := manager.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	service := NewService(settings.Cache.Directory, manager)
	<-service.restoreDone
	service.mu.Lock()
	service.schedule = endedSchedule(now, 3, now.Add(-6*time.Hour))
	service.mu.Unlock()

	service.RefreshIfExhausted("test")

	deadline := time.Now().Add(5 * time.Second)
	for {
		nowPlaying := service.GetNowPlaying([]string{"game-0"})
		if len(nowPlaying) == 1 && nowPlaying[0].Current != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exhausted guide was not refreshed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for service.GetStatus().Refreshing {
		time.Sleep(10 * time.Millisecond)
	}

	// Even if the guide runs dry again, a second automatic refresh waits out the interval.
	fetched := requests.Load()
	service.mu.Lock()
	service.schedule = endedSchedule(now, 3, now.Add(-6*time.Hour))
	service.mu.Unlock()
	service.RefreshIfExhausted("test")
	time.Sleep(100 * time.Millisecond)
	if got := requests.Load(); got != fetched {
		t.Fatalf("guide requests = %d after throttled call, want %d", got, fetched)
	}
}

func TestRefreshIfExhaustedSkipsRecentlyUpdatedGuide(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: endedSchedule(now, 3, now.Add(-5*time.Minute))}
	service.RefreshIfExhausted("test")
	if !service.lastExhaustedRefresh.IsZero() {
		t.Fatal("guide refreshed minutes ago should not be refetched")
	}
}
