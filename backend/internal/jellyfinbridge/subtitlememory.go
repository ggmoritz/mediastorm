package jellyfinbridge

import (
	"encoding/json"
	"strings"

	"novastream/services/playback"
	"novastream/utils/language"
)

// Remembered subtitle choice per profile and series. The bridge reports
// RememberSubtitleSelections as on, and clients rely on it: on the next
// episode Plezy carries the subtitle by language only when it finds a track
// of the same language and forced-ness, and otherwise takes the server's
// default, which came from mediastorm's own subtitle settings and dropped the
// viewer's pick. The pick is learned from progress reports (they carry the
// selected stream index) and becomes the next episode's default when a
// track of the same language and forced-ness exists; "off" carries too.
// Only real picks are learned: a report that just echoes the default this
// play session was handed is not one.

// maxSubtitleChoicesPerProfile bounds the remembered series per profile.
const maxSubtitleChoicesPerProfile = 5000

type subtitleChoice struct {
	Off      bool   `json:"off,omitempty"`
	Language string `json:"language,omitempty"` // normalized code
	Forced   bool   `json:"forced,omitempty"`
}

func subtitleMemoryKey(profileID, seriesSource string) string {
	return prefsKey(profileID, "subtitle:"+seriesSource, "")
}

// subtitleLanguage normalizes a track language so "ger", "deu" and "de"
// compare equal.
func subtitleLanguage(lang string) string {
	if code := language.NormalizeToCode(lang); code != "" {
		return code
	}
	return strings.ToLower(strings.TrimSpace(lang))
}

func subtitleTrackIndex(sub playback.SubtitleTrackInfo) int {
	if sub.AbsoluteIndex > 0 {
		return sub.AbsoluteIndex
	}
	return sub.Index
}

// rememberSubtitle stores the subtitle a progress report says is selected
// and says what it did, for the track diagnostics in the log.
func (s *Server) rememberSubtitle(p *principal, sess *playSession, index int) string {
	if s.subtitles == nil {
		return "memory disabled"
	}
	if sess.Ref.Kind != refEpisode {
		return "not an episode"
	}
	sess.mu.Lock()
	status := sess.status
	if !sess.subtitlePicked {
		// Until the viewer changes the subtitle, the client reports the
		// default the bridge handed out (often "off" while tracks were still
		// unknown); remembering that would override the server's own default
		// for the whole series.
		if sess.serverSubtitle != nil && *sess.serverSubtitle == index {
			sess.mu.Unlock()
			return "server default, not a pick"
		}
		sess.subtitlePicked = true
	}
	sess.mu.Unlock()
	choice := subtitleChoice{Off: index < 0}
	if !choice.Off {
		if status == nil {
			return "no stream status"
		}
		found := false
		for _, sub := range status.SubtitleTracks {
			if subtitleTrackIndex(sub) == index {
				choice.Language, choice.Forced, found = subtitleLanguage(sub.Language), sub.Forced, true
				break
			}
		}
		if !found {
			return "track not in the stream"
		}
		if choice.Language == "" || choice.Language == "und" {
			return "track has no language" // nothing to recognise it by on another episode
		}
	}
	key := subtitleMemoryKey(p.Profile.ID, seriesInfoFromSeasonRef(sess.Ref).source())
	if old, ok := s.subtitles.get(key); ok {
		var prev subtitleChoice
		if json.Unmarshal(old, &prev) == nil && prev.Off == choice.Off && prev.Language == choice.Language && prev.Forced == choice.Forced {
			return "unchanged" // progress reports repeat it every few seconds
		}
	}
	raw, _ := json.Marshal(choice)
	if !s.subtitles.set(key, raw) {
		return "store full"
	}
	if choice.Off {
		return "stored off"
	}
	return "stored " + choice.Language
}

// rememberedSubtitle returns the default subtitle index for an episode from
// the profile's last pick in the series: -1 when subtitles were turned off,
// the matching track otherwise; ok is false when nothing applies.
func (s *Server) rememberedSubtitle(profileID string, ref itemRef, status *playback.PrequeueStatusResponse) (index int, ok bool) {
	if s.subtitles == nil || ref.Kind != refEpisode || status == nil {
		return 0, false
	}
	raw, found := s.subtitles.get(subtitleMemoryKey(profileID, seriesInfoFromSeasonRef(ref).source()))
	if !found {
		return 0, false
	}
	var choice subtitleChoice
	if json.Unmarshal(raw, &choice) != nil {
		return 0, false
	}
	if choice.Off {
		return -1, true
	}
	for _, sub := range status.SubtitleTracks {
		if subtitleLanguage(sub.Language) == choice.Language && sub.Forced == choice.Forced {
			return subtitleTrackIndex(sub), true
		}
	}
	return 0, false
}
