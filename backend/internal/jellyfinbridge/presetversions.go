package jellyfinbridge

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Quality preset versions: besides "Auto" and the individual releases, the
// version picker offers the quality presets the admin chose for pickers
// ("Auto · German HD"). Such a version plays like Auto (prequeue, and with
// Auto: Avoid Video Transcoding the walk down the releases), ranked by that
// preset instead of the one the profile and device use. mediastorm decides
// per title which presets fit (anime presets only on anime) and which one
// Auto uses, so the choices are fetched per item.

const (
	presetChoicesTTL = 2 * time.Minute
	// A failed lookup is cached too, but retried sooner.
	presetChoicesRetry = 15 * time.Second
	maxPresetChoices   = 5000
	// presetLookupWait bounds the preset lookup while listing an item's
	// versions.
	presetLookupWait = 3 * time.Second
)

// presetsInUse reports whether mediastorm has any quality preset.
func (s *Server) presetsInUse() bool {
	return s.deps.QualityPresetsInUse == nil || s.deps.QualityPresetsInUse()
}

// presetChoice is a quality preset offered in version pickers.
type presetChoice struct {
	ID        string `json:"id"` // preset ID or "simple"
	Name      string `json:"name"`
	MediaType string `json:"mediaType"` // movie, series, or "" for both
}

// presetSelection is the preset Auto uses for an item ("" = simple ranking).
type presetSelection struct {
	PresetID string `json:"presetId"`
	Name     string `json:"name"`
	Source   string `json:"source"`
}

// itemPresets are the picker choices and Auto's preset for one item.
type itemPresets struct {
	offered []presetChoice
	auto    presetSelection
	// def is Auto's preset without the one remembered for the title;
	// playing it clears the memory. Source is "" when unknown.
	def     presetSelection
	fetched time.Time
	gen     uint64 // ranking generation it was fetched under
}

type presetChoiceCache struct {
	mu    sync.Mutex
	items map[string]itemPresets // profile|device|item
}

// presetsForItem returns the presets offered for an item and the one Auto
// uses, cached briefly per profile, device and item.
func (s *Server) presetsForItem(ctx context.Context, p *principal, itemID string, ref itemRef) itemPresets {
	if s.deps.Handlers.QualityPresets == nil || (ref.Kind != refMovie && ref.Kind != refEpisode) || !s.presetsInUse() {
		return itemPresets{}
	}
	key := deviceKey(p) + "|" + itemID
	c := &s.presetChoices
	c.mu.Lock()
	gen := s.rankingGeneration()
	if cached, ok := c.items[key]; ok && time.Since(cached.fetched) < presetChoicesTTL && cached.gen == gen {
		c.mu.Unlock()
		return cached
	}
	c.mu.Unlock()

	q := url.Values{"userId": {p.Profile.ID}}
	if p.Client.DeviceID != "" {
		q.Set("clientId", "jellyfin-"+p.Client.DeviceID)
	}
	if req, err := s.prequeueRequest(ctx, p, ref); err == nil {
		q.Set("mediaType", req.MediaType)
		q.Set("titleId", req.TitleID)
		q.Set("name", req.TitleName)
		if req.Year > 0 {
			q.Set("year", strconv.Itoa(req.Year))
		}
		if req.ImdbID != "" {
			q.Set("imdbId", req.ImdbID)
		}
	}
	var resp struct {
		Offered       []presetChoice  `json:"offered"`
		Movie         presetSelection `json:"movie"`
		Series        presetSelection `json:"series"`
		MovieDefault  presetSelection `json:"movieDefault"`
		SeriesDefault presetSelection `json:"seriesDefault"`
	}
	var out itemPresets
	if _, err := callJSON(s.deps.Handlers.QualityPresets, internalRequest(ctx, p, http.MethodGet, "/api/quality-presets?"+q.Encode(), nil, nil), &resp); err != nil {
		out = itemPresets{fetched: time.Now().Add(presetChoicesRetry - presetChoicesTTL), gen: gen}
	} else {
		out = itemPresets{offered: resp.Offered, auto: resp.Series, def: resp.SeriesDefault, fetched: time.Now(), gen: gen}
		if ref.Kind == refMovie {
			out.auto, out.def = resp.Movie, resp.MovieDefault
		}
	}
	c.mu.Lock()
	if c.items == nil || len(c.items) >= maxPresetChoices {
		c.items = map[string]itemPresets{}
	}
	c.items[key] = out
	c.mu.Unlock()
	return out
}

// presetVersions lists the offered presets that fit an item.
func (s *Server) presetVersions(ctx context.Context, p *principal, itemID string, ref itemRef) []presetChoice {
	mediaType := "series"
	if ref.Kind == refMovie {
		mediaType = "movie"
	}
	var out []presetChoice
	for _, c := range s.presetsForItem(ctx, p, itemID, ref).offered {
		if c.ID != "" && (c.MediaType == "" || c.MediaType == mediaType) {
			out = append(out, c)
		}
	}
	return out
}

// autoVersionLabel names the automatic version after the preset it uses;
// without any preset it keeps its original name.
func (s *Server) autoVersionLabel(ctx context.Context, p *principal, itemID string, ref itemRef) string {
	if !s.presetsInUse() {
		return "Auto (mediastorm)"
	}
	if auto := s.presetsForItem(ctx, p, itemID, ref).auto; auto.PresetID != "" && auto.Name != "" {
		return "Auto · " + auto.Name
	}
	return "Auto"
}

// forgetAutoRanking drops what the bridge cached for the automatic versions
// of one device (deviceKey): Auto's label, its picks and the release
// lists it ranks. It runs when a preset version is played, since
// mediastorm then remembers that preset for the title on that device and
// Auto ranks the title's items there with it.
func (s *Server) forgetAutoRanking(device string) {
	prefix := device + "|"
	auto := func(key string) bool { return strings.HasPrefix(key, prefix) && strings.HasSuffix(key, "|") }
	s.presetChoices.mu.Lock()
	for key := range s.presetChoices.items {
		if strings.HasPrefix(key, prefix) {
			delete(s.presetChoices.items, key)
		}
	}
	s.presetChoices.mu.Unlock()
	s.autoPicks.mu.Lock()
	for key := range s.autoPicks.byKey {
		if auto(key) {
			delete(s.autoPicks.byKey, key)
		}
	}
	s.autoPicks.mu.Unlock()
	s.releases.mu.Lock()
	for key := range s.releases.lists {
		if auto(key) {
			delete(s.releases.lists, key)
		}
	}
	s.releases.mu.Unlock()
}

// presetMediaSourceID is the media source ID of a preset version.
func (s *Server) presetMediaSourceID(p *principal, itemID, presetID string) string {
	return s.mediaSourceID(p.Profile.ID, p.Client.DeviceID, itemID+"|preset|"+presetID)
}

// presetByMediaSource finds the preset a media source ID stands for.
func (s *Server) presetByMediaSource(ctx context.Context, p *principal, itemID string, ref itemRef, mediaSourceID string) (presetChoice, bool) {
	mediaSourceID = strings.ToLower(strings.TrimSpace(mediaSourceID))
	if mediaSourceID == "" {
		return presetChoice{}, false
	}
	for _, c := range s.presetVersions(ctx, p, itemID, ref) {
		if s.presetMediaSourceID(p, itemID, c.ID) == mediaSourceID {
			return c, true
		}
	}
	return presetChoice{}, false
}

// presetVersionLabel names a version that ranks with an explicitly picked
// preset. "Preset" sets it apart from Auto, which is named after the preset
// it uses ("Auto · German HD") and can be the same one. The preset the item
// gets without a remembered one is marked "(default)": playing it makes the
// title follow the defaults again.
func presetVersionLabel(c presetChoice, def presetSelection) string {
	label := "Preset · " + c.Name
	if def.Source != "" && presetIDsEqual(c.ID, def.PresetID) {
		label += " (default)"
	}
	return label
}

// presetIDsEqual compares preset IDs; "simple" and "" both mean the simple
// ranking.
func presetIDsEqual(a, b string) bool {
	norm := func(id string) string {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "simple" {
			return ""
		}
		return id
	}
	return norm(a) == norm(b)
}
