package jellyfinbridge

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"novastream/config"
	"novastream/internal/jellyfinbridge/dto"
	"novastream/services/playback"
)

// The subtitle picked in one episode becomes the next episode's default
// (Jellyfin's RememberSubtitleSelections, which the bridge reports as on):
// clients such as Plezy fall back to the server's default when they cannot
// carry the pick themselves.
func TestSubtitlePickCarriesToNextEpisode(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	installFakePlayback(t, h)
	// The second episode's release orders its tracks differently and tags
	// German as "deu" instead of "ger"; the third has only forced German.
	var calls atomic.Int32
	h.srv.deps.Handlers.PrequeueStatus = func(w http.ResponseWriter, r *http.Request) {
		subs := []playback.SubtitleTrackInfo{
			{Index: 0, AbsoluteIndex: 3, Language: "eng", Codec: "subrip"},
			{Index: 1, AbsoluteIndex: 4, Language: "ger", Codec: "subrip"},
			{Index: 2, AbsoluteIndex: 5, Language: "ger", Codec: "subrip", Forced: true},
		}
		switch calls.Load() {
		case 1:
			subs = []playback.SubtitleTrackInfo{
				{Index: 0, AbsoluteIndex: 3, Language: "deu", Codec: "ass", Forced: true},
				{Index: 1, AbsoluteIndex: 4, Language: "eng", Codec: "ass"},
				{Index: 2, AbsoluteIndex: 5, Language: "deu", Codec: "ass"},
			}
		case 2:
			subs = []playback.SubtitleTrackInfo{{Index: 0, AbsoluteIndex: 3, Language: "ger", Codec: "subrip", Forced: true}}
		}
		writeJSON(w, http.StatusOK, playback.PrequeueStatusResponse{
			PrequeueID: "pq1", Status: playback.PrequeueStatusReady, StreamPath: "/webdav/episode.mkv",
			SelectedAudioTrack: 1, SelectedSubtitleTrack: -1, SubtitleTracks: subs,
			AudioTracks: []playback.AudioTrackInfo{{Index: 1, Codec: "aac", Language: "ger"}},
		})
	}
	tok := h.login("Living Room", "alice-pass")
	next := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/NextUp", tok, nil))
	seriesID := next.Items[0].SeriesID
	seasons := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/"+seriesID+"/Seasons", tok, nil))
	eps := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/"+seriesID+"/Episodes?SeasonId="+seasons.Items[0].ID, tok, nil))
	first, second := eps.Items[0].ID, eps.Items[1].ID
	play := func(item string, release int32) dto.PlaybackInfoResponse {
		calls.Store(release)
		return decode[dto.PlaybackInfoResponse](t, h.do(http.MethodPost, "/Items/"+item+"/PlaybackInfo", tok, map[string]any{}))
	}
	defaultSub := func(info dto.PlaybackInfoResponse) int {
		if d := info.MediaSources[0].DefaultSubtitleStreamIndex; d != nil {
			return *d
		}
		return -1
	}
	report := func(item string, info dto.PlaybackInfoResponse, sub int) {
		calls.Store(-1) // keep the session's own status
		h.do(http.MethodPost, "/Sessions/Playing/Progress", tok, map[string]any{"ItemId": item, "PlaySessionId": info.PlaySessionID, "PositionTicks": int64(60) * ticksPerSecond, "SubtitleStreamIndex": sub})
	}

	one := play(first, 0)
	if got := defaultSub(one); got != -1 {
		t.Fatalf("first episode default subtitle = %d, want mediastorm's (off)", got)
	}
	report(first, one, 4) // German, full

	two := play(second, 1)
	if got := defaultSub(two); got != 5 {
		t.Fatalf("next episode default subtitle = %d, want the German full track (5)", got)
	}
	for _, st := range two.MediaSources[0].MediaStreams {
		if st.Type == "Subtitle" && st.IsDefault != (st.Index == 5) {
			t.Fatalf("stream %d IsDefault = %v", st.Index, st.IsDefault)
		}
	}

	// Only forced German: the pick does not apply, mediastorm's default stays.
	h.srv.plays = newPlaySessions()
	if got := defaultSub(play(second, 2)); got != -1 {
		t.Fatalf("with only forced German the default = %d, want mediastorm's (off)", got)
	}

	// Turning subtitles off carries too.
	h.srv.plays = newPlaySessions()
	two = play(second, 1)
	report(second, two, -1)
	h.srv.plays = newPlaySessions()
	if got := defaultSub(play(first, 0)); got != -1 {
		t.Fatalf("after turning subtitles off the default = %d, want off", got)
	}
	if v, ok := h.srv.tracks.get(subtitleMemoryKey(h.alice.ID, seriesInfoFromSeasonRef(mustRef(t, h.srv, second)).source())); !ok || !strings.Contains(string(v), `"off":true`) {
		t.Fatalf("stored choice = %s", v)
	}
}

func mustRef(t *testing.T, s *Server, id string) itemRef {
	t.Helper()
	ref, ok := s.ids.get(id)
	if !ok {
		t.Fatalf("unknown item %s", id)
	}
	return ref
}

// Plezy leaves PlaySessionId out of its playback reports; the bridge finds
// the session by device and item, as Jellyfin does, so the pick is learned.
func TestReportWithoutPlaySessionIDFindsTheSession(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	installFakePlayback(t, h)
	h.srv.deps.Handlers.PrequeueStatus = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, playback.PrequeueStatusResponse{
			PrequeueID: "pq1", Status: playback.PrequeueStatusReady, StreamPath: "/webdav/episode.mkv",
			SelectedAudioTrack: 1, SelectedSubtitleTrack: -1,
			SubtitleTracks: []playback.SubtitleTrackInfo{{Index: 0, AbsoluteIndex: 3, Language: "ger", Codec: "subrip"}},
			AudioTracks:    []playback.AudioTrackInfo{{Index: 1, Codec: "aac", Language: "ger"}},
		})
	}
	tok := h.login("Living Room", "alice-pass")
	next := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/NextUp", tok, nil))
	item := next.Items[0].ID
	info := decode[dto.PlaybackInfoResponse](t, h.do(http.MethodPost, "/Items/"+item+"/PlaybackInfo", tok, map[string]any{}))
	h.do(http.MethodPost, "/Sessions/Playing/Progress", tok, map[string]any{
		"ItemId": item, "MediaSourceId": info.MediaSources[0].ID, "PositionTicks": int64(60) * ticksPerSecond, "SubtitleStreamIndex": 3,
	})
	key := subtitleMemoryKey(h.alice.ID, seriesInfoFromSeasonRef(mustRef(t, h.srv, item)).source())
	if v, ok := h.srv.tracks.get(key); !ok || !strings.Contains(string(v), `"language":"deu"`) {
		t.Fatalf("subtitle pick from a report without PlaySessionId not learned: %s", v)
	}
	sess, _ := h.srv.plays.get(info.PlaySessionID)
	if sess == nil || sess.state == nil || sess.state.Subtitle == nil || *sess.state.Subtitle != 3 {
		t.Fatalf("session state not recorded: %+v", sess)
	}
}

// forReport prefers the session of the reported media source (Auto and
// versions have their own) and otherwise takes the newest one.
func TestForReportPrefersReportedMediaSource(t *testing.T) {
	ps := newPlaySessions()
	now := time.Now()
	const itemID, versionID = "0f751e0e9b925b4da5d502b33c8770e6", "5951ff4d7440431daf8378ebfb26296b"
	auto := &playSession{ID: "auto", ProfileID: "p", DeviceID: "d", ItemID: itemID, MediaSourceID: itemID, created: now.Add(-time.Minute)}
	version := &playSession{ID: "version", ProfileID: "p", DeviceID: "d", ItemID: itemID, MediaSourceID: versionID, created: now}
	other := &playSession{ID: "other-device", ProfileID: "p", DeviceID: "x", ItemID: itemID, created: now.Add(time.Minute)}
	ps.add(auto)
	ps.add(version)
	ps.add(other)
	if got, _ := ps.forReport("p", "d", itemID, itemID); got != auto {
		t.Errorf("reported Auto source: got %v", got)
	}
	if got, _ := ps.forReport("p", "d", itemID, ""); got != version {
		t.Errorf("no source: want the newest session on the device, got %v", got)
	}
	if got, ok := ps.forReport("p", "d", "other-item", ""); ok {
		t.Errorf("other item matched %v", got)
	}
}

// A stop report without PlaySessionId ends the play session, and a report
// that only echoes the default subtitle the bridge handed out is not
// remembered as a pick.
func TestReportsWithoutPlaySessionIDStopAndOnlyLearnPicks(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	installFakePlayback(t, h)
	h.srv.deps.Handlers.PrequeueStatus = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, playback.PrequeueStatusResponse{
			PrequeueID: "pq1", Status: playback.PrequeueStatusReady, StreamPath: "/webdav/episode.mkv",
			SelectedAudioTrack: 1, SelectedSubtitleTrack: -1,
			SubtitleTracks: []playback.SubtitleTrackInfo{{Index: 0, AbsoluteIndex: 3, Language: "ger", Codec: "subrip", Forced: true}},
			AudioTracks:    []playback.AudioTrackInfo{{Index: 1, Codec: "aac", Language: "ger"}},
		})
	}
	tok := h.login("Living Room", "alice-pass")
	next := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/NextUp", tok, nil))
	item := next.Items[0].ID
	info := decode[dto.PlaybackInfoResponse](t, h.do(http.MethodPost, "/Items/"+item+"/PlaybackInfo", tok, map[string]any{}))
	report := func(path string, sub int) {
		h.do(http.MethodPost, path, tok, map[string]any{"ItemId": item, "PositionTicks": int64(60) * ticksPerSecond, "SubtitleStreamIndex": sub})
	}
	key := subtitleMemoryKey(h.alice.ID, seriesInfoFromSeasonRef(mustRef(t, h.srv, item)).source())

	report("/Sessions/Playing", -1) // the default the bridge handed out
	report("/Sessions/Playing/Progress", -1)
	if v, ok := h.srv.tracks.get(key); ok {
		t.Fatalf("the server's default was remembered as a pick: %s", v)
	}
	report("/Sessions/Playing/Progress", 3)  // the viewer turns on forced German
	report("/Sessions/Playing/Progress", -1) // and off again: that is a pick too
	if v, ok := h.srv.tracks.get(key); !ok || !strings.Contains(string(v), `"off":true`) {
		t.Fatalf("stored choice = %s", v)
	}

	report("/Sessions/Playing/Stopped", -1)
	if _, ok := h.srv.plays.get(info.PlaySessionID); ok {
		t.Error("a stop report without PlaySessionId must end the play session")
	}
}

// The audio language picked in an episode becomes the default when the
// episode is opened again and in the series' next episodes (Jellyfin's
// RememberAudioSelections): Plezy only carries it when it moves to the next
// episode itself.
func TestAudioPickCarriesAcrossEpisodes(t *testing.T) {
	h := newHarness(t, config.JellyfinBridgeSettings{})
	installFakePlayback(t, h)
	// mediastorm defaults to English. The second episode's release has two
	// Japanese tracks; the third has none.
	var calls atomic.Int32
	h.srv.deps.Handlers.PrequeueStatus = func(w http.ResponseWriter, r *http.Request) {
		audio := []playback.AudioTrackInfo{{Index: 1, Codec: "eac3", Language: "ger"}, {Index: 2, Codec: "eac3", Language: "jpn"}, {Index: 3, Codec: "eac3", Language: "eng"}}
		selected := 3
		switch calls.Load() {
		case 1:
			audio = []playback.AudioTrackInfo{{Index: 1, Codec: "aac", Language: "eng"}, {Index: 2, Codec: "aac", Language: "jpn"}, {Index: 3, Codec: "flac", Language: "jpn"}}
			selected = 1
		case 2:
			audio = []playback.AudioTrackInfo{{Index: 1, Codec: "aac", Language: "deu"}, {Index: 2, Codec: "aac", Language: "eng"}}
			selected = 2
		}
		writeJSON(w, http.StatusOK, playback.PrequeueStatusResponse{
			PrequeueID: "pq1", Status: playback.PrequeueStatusReady, StreamPath: "/webdav/episode.mkv",
			SelectedAudioTrack: selected, SelectedSubtitleTrack: -1, AudioTracks: audio,
		})
	}
	tok := h.login("Living Room", "alice-pass")
	next := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/NextUp", tok, nil))
	seriesID := next.Items[0].SeriesID
	seasons := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/"+seriesID+"/Seasons", tok, nil))
	eps := decode[dto.QueryResult[dto.BaseItemDto]](t, h.do(http.MethodGet, "/Shows/"+seriesID+"/Episodes?SeasonId="+seasons.Items[0].ID, tok, nil))
	first, second := eps.Items[0].ID, eps.Items[1].ID
	play := func(item string, release int32) int {
		calls.Store(release)
		info := decode[dto.PlaybackInfoResponse](t, h.do(http.MethodPost, "/Items/"+item+"/PlaybackInfo", tok, map[string]any{}))
		if d := info.MediaSources[0].DefaultAudioStreamIndex; d != nil {
			return *d
		}
		return -1
	}
	report := func(item string, audio int) {
		calls.Store(-1) // keep the session's own status
		h.do(http.MethodPost, "/Sessions/Playing/Progress", tok, map[string]any{"ItemId": item, "PositionTicks": int64(60) * ticksPerSecond, "AudioStreamIndex": audio})
	}
	key := audioMemoryKey(h.alice.ID, seriesInfoFromSeasonRef(mustRef(t, h.srv, first)).source())

	if got := play(first, 0); got != 3 {
		t.Fatalf("first play default audio = %d, want mediastorm's English (3)", got)
	}
	report(first, 3) // the default the bridge handed out
	if v, ok := h.srv.tracks.get(key); ok {
		t.Fatalf("the server's default was remembered as a pick: %s", v)
	}
	report(first, 2) // the viewer switches to Japanese
	if v, ok := h.srv.tracks.get(key); !ok || !strings.Contains(string(v), `"language":"jpn"`) {
		t.Fatalf("stored choice = %s", v)
	}

	// Opening the episode again (Plezy reuses the play session).
	if got := play(first, 0); got != 2 {
		t.Fatalf("reopened episode default audio = %d, want Japanese (2)", got)
	}
	// The next episode: the first Japanese track, as mediastorm's default is English.
	h.srv.plays = newPlaySessions()
	if got := play(second, 1); got != 2 {
		t.Fatalf("next episode default audio = %d, want the first Japanese track (2)", got)
	}
	// No Japanese track: mediastorm's default stays.
	h.srv.plays = newPlaySessions()
	if got := play(second, 2); got != 2 {
		t.Fatalf("without Japanese the default = %d, want mediastorm's English (2)", got)
	}
}

// A Japanese default mediastorm picked stays when Japanese is remembered.
func TestRememberedAudioKeepsMediastormsTrackOfThatLanguage(t *testing.T) {
	s := &Server{tracks: newNamedPrefsStore("", "track choices", maxTrackChoicesPerProfile)}
	t.Cleanup(s.tracks.close)
	ref := itemRef{Kind: refEpisode, Source: "tmdb:tv:1:s1e1"}
	s.tracks.set(audioMemoryKey("p", seriesInfoFromSeasonRef(ref).source()), []byte(`{"language":"jpn"}`))
	status := &playback.PrequeueStatusResponse{
		SelectedAudioTrack: 3,
		AudioTracks:        []playback.AudioTrackInfo{{Index: 1, Language: "eng"}, {Index: 2, Language: "jpn"}, {Index: 3, Language: "ja"}},
	}
	if got, ok := s.rememberedAudio("p", ref, status); !ok || got != 3 {
		t.Fatalf("rememberedAudio = %d, %v; want mediastorm's Japanese track 3", got, ok)
	}
}

// A client that sends the default it was handed back with its next
// PlaybackInfo has not picked anything.
func TestPickStateIgnoresRequestedDefault(t *testing.T) {
	var ps pickState
	ps.handedOut(3, false)
	ps.handedOut(3, true)
	if ps.reported(3) {
		t.Fatal("the echoed default counted as a pick")
	}
	if !ps.reported(2) || !ps.reported(3) {
		t.Fatal("a changed track is a pick, and later reports stay picks")
	}
	ps.handedOut(2, true)
	if !ps.picked {
		t.Fatal("asking for another track is a pick")
	}
}
