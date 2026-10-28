package jellyfinbridge

import (
	"testing"

	"novastream/services/playback"
)

// Track diagnostics are logged once per change and name the tracks.
func TestTrackDiagnostics(t *testing.T) {
	sess := &playSession{}
	one, two := 1, 2
	if !sess.tracksChanged(playbackReport{AudioIndex: &one}) {
		t.Error("the first report counts as a change")
	}
	sess.recordState(playbackReport{AudioIndex: &one})
	if sess.tracksChanged(playbackReport{AudioIndex: &one}) {
		t.Error("a repeated pick is no change")
	}
	if !sess.tracksChanged(playbackReport{AudioIndex: &two}) {
		t.Error("a new audio pick is a change")
	}

	status := &playback.PrequeueStatusResponse{
		AudioTracks:    []playback.AudioTrackInfo{{Index: 1, Language: "ger", Codec: "eac3"}},
		SubtitleTracks: []playback.SubtitleTrackInfo{{Index: 0, AbsoluteIndex: 3, Language: "eng", Forced: true}},
	}
	for _, tc := range []struct {
		got, want string
	}{
		{describeTrack(status, 1, true), "1 ger eac3"},
		{describeTrack(status, 3, false), "3 eng forced"},
		{describeTrack(status, -1, false), "-1 off"},
		{describeTrack(status, 7, true), "7 (not in the stream)"},
		{describeTracks(status), "audio [1:ger] subtitles [3:eng(forced)]"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
