package jellyfinbridge

import (
	"encoding/json"
	"strings"

	"novastream/services/playback"
	"novastream/utils/language"
)

// Remembered audio and subtitle choices per profile and series. The bridge
// reports RememberAudioSelections and RememberSubtitleSelections as on, and
// clients rely on it: Plezy carries a pick to the next episode only when it
// navigates there itself and finds a matching track, and otherwise (opening
// an episode again, a matching track missing) takes the server's default,
// which came from mediastorm's own settings and dropped the viewer's pick.
// Picks are learned from progress reports (they carry the selected stream
// indexes) and become the defaults of the series' episodes when a matching
// track exists; a subtitle turned off carries too. Only real picks are
// learned: a report that just echoes the default this play session was
// handed is not one.

// maxTrackChoicesPerProfile bounds the remembered choices per profile (an
// audio and a subtitle choice per series).
const maxTrackChoicesPerProfile = 10000

type subtitleChoice struct {
	Off      bool   `json:"off,omitempty"`
	Language string `json:"language,omitempty"` // normalized code
	Forced   bool   `json:"forced,omitempty"`
}

type audioChoice struct {
	Language string `json:"language"` // normalized code
}

func subtitleMemoryKey(profileID, seriesSource string) string {
	return prefsKey(profileID, "subtitle:"+seriesSource, "")
}

func audioMemoryKey(profileID, seriesSource string) string {
	return prefsKey(profileID, "audio:"+seriesSource, "")
}

// trackLanguage normalizes a track language so "ger", "deu" and "de" compare
// equal.
func trackLanguage(lang string) string {
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

// pickState tells a viewer's pick from the default PlaybackInfo handed out
// for one track type of a play session.
type pickState struct {
	server  *int // default handed out; nil when the client asked for a track
	picked  bool // the viewer chose a track in this play session
	pending bool // the stream's tracks were unknown when the default was handed out
}

// handedOut records the track a PlaybackInfo hands out. fallback is the
// default the bridge would have handed out without the client's request, and
// known says whether the stream's tracks were known when it was chosen. A
// track the client asked for is a pick unless it repeats that default or the
// one handed out before: some clients send their current selection with the
// first PlaybackInfo of a session.
func (ps *pickState) handedOut(index, fallback int, requested, known bool) {
	if requested {
		if ps.server != nil && *ps.server == index {
			return
		}
		if !ps.picked && index == fallback {
			ps.server, ps.pending = intPtr(index), !known
			return
		}
		ps.server, ps.picked, ps.pending = nil, true, false
		return
	}
	ps.server, ps.picked, ps.pending = intPtr(index), false, !known
}

// reported says whether a reported track is the viewer's pick. Until the
// viewer changes it, the client reports the default the bridge handed out
// (often subtitles off while tracks were still unknown); remembering that
// would override the server's own default for the whole series. When the
// tracks were unknown, the bridge handed out a placeholder and the stream
// starts on mediastorm's own default, so the first report sets the default
// to compare against instead of counting as a pick.
func (ps *pickState) reported(index int) bool {
	if ps.picked {
		return true
	}
	if ps.pending {
		ps.server, ps.pending = intPtr(index), false
		return false
	}
	if ps.server != nil && *ps.server == index {
		return false
	}
	ps.picked = true
	return true
}

// rememberSubtitle stores the subtitle a progress report says is selected
// and says what it did, for the track diagnostics in the log.
func (s *Server) rememberSubtitle(p *principal, sess *playSession, index int) string {
	if s.tracks == nil {
		return "memory disabled"
	}
	if sess.Ref.Kind != refEpisode {
		return "not an episode"
	}
	sess.mu.Lock()
	status := sess.status
	pick := sess.subtitlePick.reported(index)
	sess.mu.Unlock()
	if !pick {
		return "server default, not a pick"
	}
	choice := subtitleChoice{Off: index < 0}
	if !choice.Off {
		if status == nil {
			return "no stream status"
		}
		found := false
		for _, sub := range status.SubtitleTracks {
			if subtitleTrackIndex(sub) == index {
				choice.Language, choice.Forced, found = trackLanguage(sub.Language), sub.Forced, true
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
	if old, ok := s.tracks.get(key); ok {
		var prev subtitleChoice
		if json.Unmarshal(old, &prev) == nil && prev.Off == choice.Off && prev.Language == choice.Language && prev.Forced == choice.Forced {
			return "unchanged" // progress reports repeat it every few seconds
		}
	}
	raw, _ := json.Marshal(choice)
	if !s.tracks.set(key, raw) {
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
	if s.tracks == nil || ref.Kind != refEpisode || status == nil {
		return 0, false
	}
	raw, found := s.tracks.get(subtitleMemoryKey(profileID, seriesInfoFromSeasonRef(ref).source()))
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
		if trackLanguage(sub.Language) == choice.Language && sub.Forced == choice.Forced {
			return subtitleTrackIndex(sub), true
		}
	}
	return 0, false
}

// rememberAudio stores the audio language a progress report says is
// selected and says what it did, for the track diagnostics in the log.
func (s *Server) rememberAudio(p *principal, sess *playSession, index int) string {
	if s.tracks == nil {
		return "memory disabled"
	}
	if sess.Ref.Kind != refEpisode {
		return "not an episode"
	}
	sess.mu.Lock()
	status := sess.status
	pick := sess.audioPick.reported(index)
	sess.mu.Unlock()
	if !pick {
		return "server default, not a pick"
	}
	if status == nil {
		return "no stream status"
	}
	choice, found := audioChoice{}, false
	for _, a := range status.AudioTracks {
		if a.Index == index {
			choice.Language, found = trackLanguage(a.Language), true
			break
		}
	}
	if !found {
		return "track not in the stream"
	}
	if choice.Language == "" || choice.Language == "und" {
		return "track has no language"
	}
	key := audioMemoryKey(p.Profile.ID, seriesInfoFromSeasonRef(sess.Ref).source())
	if old, ok := s.tracks.get(key); ok {
		var prev audioChoice
		if json.Unmarshal(old, &prev) == nil && prev.Language == choice.Language {
			return "unchanged"
		}
	}
	raw, _ := json.Marshal(choice)
	if !s.tracks.set(key, raw) {
		return "store full"
	}
	return "stored " + choice.Language
}

// rememberedAudio returns the default audio index for an episode from the
// profile's last pick in the series; ok is false when no track has that
// language. mediastorm's own default stays when it already has the language
// (it picked among that language's tracks, e.g. by codec).
func (s *Server) rememberedAudio(profileID string, ref itemRef, status *playback.PrequeueStatusResponse) (index int, ok bool) {
	if s.tracks == nil || ref.Kind != refEpisode || status == nil {
		return 0, false
	}
	raw, found := s.tracks.get(audioMemoryKey(profileID, seriesInfoFromSeasonRef(ref).source()))
	if !found {
		return 0, false
	}
	var choice audioChoice
	if json.Unmarshal(raw, &choice) != nil || choice.Language == "" {
		return 0, false
	}
	match := -1
	for _, a := range status.AudioTracks {
		if trackLanguage(a.Language) != choice.Language {
			continue
		}
		if a.Index == status.SelectedAudioTrack {
			return a.Index, true
		}
		if match < 0 {
			match = a.Index
		}
	}
	return match, match >= 0
}
