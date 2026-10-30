package epg

import (
	"testing"
	"time"

	"novastream/models"
)

func TestNowPlayingMatchesHDHomeRunAliases(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{
			"station-id": {ID: "station-id", Name: "5.1", Aliases: []string{"5.1", "TESTTV", "testtv", "TESTTV HD", "", " "}},
		},
		Programs: map[string][]models.EPGProgram{
			"station-id": {
				{ChannelID: "station-id", Title: "Current", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)},
				{ChannelID: "station-id", Title: "Next", Start: now.Add(time.Hour), Stop: now.Add(2 * time.Hour)},
			},
		},
	}}
	service.rebuildScheduleIndexLocked()
	for _, id := range []string{"station-id", "5.1", "TESTTV", "testtv", "TESTTV HD"} {
		t.Run(id, func(t *testing.T) {
			result := service.GetNowPlaying([]string{id})
			if len(result) != 1 || result[0].Current == nil || result[0].Current.Title != "Current" || result[0].Next == nil || result[0].Next.Title != "Next" {
				t.Fatalf("callsign/number did not resolve current and next programmes: %+v", result)
			}
			if result[0].ChannelID != id {
				t.Fatalf("request ID was changed: %q", result[0].ChannelID)
			}
			programs := service.GetSchedule(id, now.Add(-time.Hour), now.Add(time.Hour))
			if len(programs) != 1 || programs[0].ChannelID != result[0].Current.ChannelID {
				t.Fatal("now-playing and schedule resolved different stations")
			}
		})
	}
}

func TestNowPlayingRejectsAmbiguousAliases(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{
			"one":   {ID: "one", Name: "5.1", Aliases: []string{"5.1", "SHARED", "PRIMARY"}},
			"two":   {ID: "two", Name: "PRIMARY", Aliases: []string{"7.1", "shared HD"}},
			"three": {ID: "three", Name: "9.1", Aliases: []string{"SHARED"}},
		},
		Programs: map[string][]models.EPGProgram{},
	}}
	for _, id := range []string{"one", "two", "three"} {
		service.schedule.Programs[id] = []models.EPGProgram{{ChannelID: id, Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}}
	}
	for _, id := range []string{"SHARED", "PRIMARY", "", "!!!", "missing"} {
		result := service.GetNowPlaying([]string{id})
		if len(result) != 1 || result[0].Current != nil || result[0].Next != nil {
			t.Errorf("ambiguous or empty alias %q unexpectedly resolved: %+v", id, result)
		}
	}
	for _, id := range []string{"one", "two", "three", "5.1", "7.1", "9.1"} {
		result := service.GetNowPlaying([]string{id})
		if len(result) != 1 || result[0].Current == nil {
			t.Errorf("unique identifier %q failed: %+v", id, result)
		}
	}
}

func TestScheduleDoesNotSubstringMatchNumericChannelIDs(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{},
		Programs: map[string][]models.EPGProgram{},
	}}
	for _, id := range []string{"1", "2", "3"} {
		service.schedule.Channels[id] = models.EPGChannel{ID: id, Name: "Channel " + id}
		service.schedule.Programs[id] = []models.EPGProgram{{ChannelID: id, Title: "Show " + id, Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}}
	}
	service.rebuildScheduleIndexLocked()

	schedules := service.GetScheduleMultiple([]string{"1", "2", "12", "21", "30"}, now.Add(-time.Hour), now.Add(time.Hour))
	for _, id := range []string{"1", "2"} {
		if len(schedules[id]) != 1 || schedules[id][0].ChannelID != id {
			t.Errorf("channel %q should resolve to its own guide entry: %+v", id, schedules[id])
		}
	}
	for _, id := range []string{"12", "21", "30"} {
		if len(schedules[id]) != 0 {
			t.Errorf("channel %q has no guide entry but borrowed %+v", id, schedules[id])
		}
	}
}

func TestScheduleRejectsAmbiguousAliases(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{
			"one": {ID: "one", Name: "SHARED"},
			"two": {ID: "two", Name: "shared HD"},
		},
		Programs: map[string][]models.EPGProgram{},
	}}
	for _, id := range []string{"one", "two"} {
		service.schedule.Programs[id] = []models.EPGProgram{{ChannelID: id, Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}}
	}
	service.rebuildScheduleIndexLocked()
	// Only the normalized form is shared; an input matching neither name exactly stays unresolved.
	if programs := service.GetSchedule("Shared FHD", now.Add(-time.Hour), now.Add(time.Hour)); len(programs) != 0 {
		t.Fatalf("ambiguous alias resolved to %+v", programs)
	}
	// An exact display name still identifies its own channel.
	if programs := service.GetSchedule("SHARED", now.Add(-time.Hour), now.Add(time.Hour)); len(programs) != 1 || programs[0].ChannelID != "one" {
		t.Fatalf("exact display name resolved to %+v", programs)
	}
}

func TestChannelIconsMatchesHDHomeRunAliases(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{
			"station-id": {ID: "station-id", Name: "5.1", Aliases: []string{"5.1", "TESTTV"}, Icon: "https://img.example/testtv.png"},
			"one":        {ID: "one", Name: "SHARED", Icon: "https://img.example/one.png"},
			"two":        {ID: "two", Name: "SHARED", Icon: "https://img.example/two.png"},
		},
		Programs: map[string][]models.EPGProgram{
			"station-id": {{ChannelID: "station-id", Title: "Current", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}},
			"one":        {{ChannelID: "one", Title: "One", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}},
			"two":        {{ChannelID: "two", Title: "Two", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}},
		},
	}}
	service.rebuildScheduleIndexLocked()
	icons := service.ChannelIcons([]string{"TESTTV", "station-id", "SHARED", "missing"})
	if icons["TESTTV"] != "https://img.example/testtv.png" || icons["station-id"] != "https://img.example/testtv.png" {
		t.Fatalf("callsign or ID did not resolve the station icon: %v", icons)
	}
	if _, ok := icons["SHARED"]; ok {
		t.Fatalf("ambiguous alias bound to an icon: %v", icons)
	}
	if len(icons) != 2 {
		t.Fatalf("unexpected icons: %v", icons)
	}
}

// "WSIM" and "WSIM-SD" both normalize to "wsim"; exact display names keep the
// tuner's main channel and its SD subchannel apart for guide and logo lookups.
func TestHDHomeRunSubchannelCallsignsResolveByExactName(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{schedule: &models.EPGSchedule{
		Channels: map[string]models.EPGChannel{
			"us41.hdhomerun.com": {ID: "us41.hdhomerun.com", Name: "4.1", Aliases: []string{"4.1", "WSIM"}, Icon: "https://img.example/41.png"},
			"us42.hdhomerun.com": {ID: "us42.hdhomerun.com", Name: "4.2", Aliases: []string{"4.2", "WSIM-SD"}, Icon: "https://img.example/42.png"},
		},
		Programs: map[string][]models.EPGProgram{
			"us41.hdhomerun.com": {{ChannelID: "us41.hdhomerun.com", Title: "Main", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}},
			"us42.hdhomerun.com": {{ChannelID: "us42.hdhomerun.com", Title: "Sub", Start: now.Add(-time.Hour), Stop: now.Add(time.Hour)}},
		},
	}}
	service.rebuildScheduleIndexLocked()

	nowPlaying := service.GetNowPlaying([]string{"WSIM", "WSIM-SD"})
	if nowPlaying[0].Current == nil || nowPlaying[0].Current.Title != "Main" || nowPlaying[1].Current == nil || nowPlaying[1].Current.Title != "Sub" {
		t.Fatalf("subchannels did not resolve their own guide: %+v", nowPlaying)
	}
	if programs := service.GetSchedule("WSIM-SD", now.Add(-time.Hour), now.Add(time.Hour)); len(programs) != 1 || programs[0].Title != "Sub" {
		t.Fatalf("schedule for WSIM-SD = %+v", programs)
	}
	icons := service.ChannelIcons([]string{"WSIM", "WSIM-SD"})
	if icons["WSIM"] != "https://img.example/41.png" || icons["WSIM-SD"] != "https://img.example/42.png" {
		t.Fatalf("subchannel icons = %v", icons)
	}
}
