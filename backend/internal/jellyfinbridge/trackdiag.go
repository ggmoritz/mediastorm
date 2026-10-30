package jellyfinbridge

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"novastream/services/playback"
)

// trackLogs turns on the per-playback track diagnostics (the defaults each
// PlaybackInfo hands out and the picks clients report): set
// STRMR_JELLYFIN_TRACK_LOGS=1 when a client loses audio or subtitle picks.
var trackLogs = func() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("STRMR_JELLYFIN_TRACK_LOGS")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}()

// tracksChanged reports whether a report's track picks differ from the last
// recorded ones (or are the first), so track diagnostics are logged once per
// change rather than every few seconds.
func (sess *playSession) tracksChanged(rep playbackReport) bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.state == nil {
		return true
	}
	differs := func(old, cur *int) bool { return cur != nil && (old == nil || *old != *cur) }
	return differs(sess.state.Audio, rep.AudioIndex) || differs(sess.state.Subtitle, rep.SubtitleIndex)
}

// describeTrack names a track index of the stream for the logs:
// "2 ger AC3", "-1 off" or "5 (not in the stream)".
func describeTrack(status *playback.PrequeueStatusResponse, index int, audio bool) string {
	if index < 0 {
		return strconv.Itoa(index) + " off"
	}
	if status != nil {
		if audio {
			for _, a := range status.AudioTracks {
				if a.Index == index {
					return fmt.Sprintf("%d %s %s", index, orUnd(a.Language), a.Codec)
				}
			}
		} else {
			for _, sub := range status.SubtitleTracks {
				if subtitleTrackIndex(sub) == index {
					forced := ""
					if sub.Forced {
						forced = " forced"
					}
					return fmt.Sprintf("%d %s%s", index, orUnd(sub.Language), forced)
				}
			}
		}
	}
	return strconv.Itoa(index) + " (not in the stream)"
}

func describeReported(status *playback.PrequeueStatusResponse, index *int, audio bool) string {
	if index == nil {
		return "-"
	}
	return describeTrack(status, *index, audio)
}

// describeTracks lists a stream's audio and subtitle tracks for the logs.
func describeTracks(status *playback.PrequeueStatusResponse) string {
	if status == nil {
		return "(no stream yet)"
	}
	var audio, subs []string
	for _, a := range status.AudioTracks {
		audio = append(audio, fmt.Sprintf("%d:%s", a.Index, orUnd(a.Language)))
	}
	for _, sub := range status.SubtitleTracks {
		entry := fmt.Sprintf("%d:%s", subtitleTrackIndex(sub), orUnd(sub.Language))
		if sub.Forced {
			entry += "(forced)"
		}
		subs = append(subs, entry)
	}
	return "audio [" + strings.Join(audio, " ") + "] subtitles [" + strings.Join(subs, " ") + "]"
}

func orUnd(lang string) string {
	if strings.TrimSpace(lang) == "" {
		return "und"
	}
	return lang
}
