package arr

import (
	"testing"
	"time"
)

func testProfile() Profile {
	return Profile{
		App:  AppSonarr,
		Name: "Test",
		Items: []ProfileItem{
			{Name: "WEB 1080p", Allowed: true, Qualities: []string{"WEBDL-1080p", "WEBRip-1080p"}},
			{Name: "Bluray-1080p", Allowed: true},
			{Name: "HDTV-1080p", Allowed: false},
			{Name: "WEB 720p", Allowed: true, Qualities: []string{"WEBDL-720p", "WEBRip-720p"}},
		},
		MinFormatScore: 0,
		FormatScores:   map[string]int{"german": 1000, "ger-dl": 500, "bad": -10000},
	}
}

func testFormats() []CustomFormat {
	return []CustomFormat{
		{TrashID: "german", Name: "German", Specifications: []Specification{
			spec("g", SpecLanguage, false, true, map[string]any{"value": 4}),
		}},
		{TrashID: "ger-dl", Name: "German DL", Specifications: []Specification{
			spec("g", SpecLanguage, false, true, map[string]any{"value": 4}),
			spec("o", SpecLanguage, false, true, map[string]any{"value": -2}),
		}},
		{TrashID: "bad", Name: "LQ", Specifications: []Specification{
			spec("grp", SpecReleaseGroup, false, true, map[string]any{"value": `^(BADGRP)$`}),
		}},
		{TrashID: "unused", Name: "Unused", Specifications: []Specification{
			spec("x", SpecReleaseTitle, false, true, map[string]any{"value": `.`}),
		}},
	}
}

func newTestEvaluator(t *testing.T, p Profile) *Evaluator {
	t.Helper()
	ev, errs := NewEvaluator(p, testFormats())
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return ev
}

func evalTitle(ev *Evaluator, title string, ctx ReleaseContext) Evaluation {
	return ev.Evaluate(ev.Prepare(Parse(AppSonarr, title), ctx))
}

func TestEvaluateRejectsUnwantedQualityAndLowScore(t *testing.T) {
	ev := newTestEvaluator(t, testProfile())
	ctx := ReleaseContext{OriginalLanguage: "English"}
	if e := evalTitle(ev, "Show.S01E01.1080p.HDTV.x264-GRP", ctx); e.Allowed {
		t.Errorf("HDTV-1080p is not allowed: %+v", e)
	}
	if e := evalTitle(ev, "Show.S01E01.2160p.WEB.h265-GRP", ctx); e.Allowed {
		t.Error("qualities missing from the profile are rejected")
	}
	e := evalTitle(ev, "Show.S01E01.1080p.WEB.h264-BADGRP", ctx)
	if e.Allowed || e.Score != -10000 {
		t.Errorf("negative score below minimum must be rejected, got %+v", e)
	}
	e = evalTitle(ev, "Show.S01E01.German.DL.1080p.WEB.h264-GRP", ctx)
	if !e.Allowed || e.Score != 1500 {
		t.Errorf("German DL should score 1500, got %+v", e)
	}
	for _, f := range e.Formats {
		if f.Key == "unused" {
			t.Error("formats without a score must not be evaluated")
		}
	}
}

func TestCompareOrdersByQualityThenScore(t *testing.T) {
	ev := newTestEvaluator(t, testProfile())
	ctx := ReleaseContext{OriginalLanguage: "English"}
	titles := []string{
		"Show.S01E01.720p.WEB.h264-GRP",
		"Show.S01E01.German.1080p.BluRay.x264-GRP",
		"Show.S01E01.1080p.WEBRip.x264-GRP",
		"Show.S01E01.German.DL.1080p.WEB.h264-GRP",
		"Show.S01E01.German.1080p.WEB.h264-GRP",
	}
	var evals []Evaluation
	for _, title := range titles {
		evals = append(evals, evalTitle(ev, title, ctx))
	}
	ev.SortEvaluations(evals)
	want := []string{
		"Show.S01E01.German.DL.1080p.WEB.h264-GRP", // WEB 1080p group, score 1500
		"Show.S01E01.German.1080p.WEB.h264-GRP",    // WEB 1080p group, score 1000
		"Show.S01E01.1080p.WEBRip.x264-GRP",        // WEB 1080p group (WEBRip ranks equal), score 0
		"Show.S01E01.German.1080p.BluRay.x264-GRP", // lower quality item despite score
		"Show.S01E01.720p.WEB.h264-GRP",
	}
	for i, e := range evals {
		if e.Release.Title != want[i] {
			t.Fatalf("position %d: got %s want %s", i, e.Release.Title, want[i])
		}
	}
}

func TestComparePreferredProtocolAndSize(t *testing.T) {
	p := testProfile()
	p.PreferredProtocol = ProtocolUsenet
	ev := newTestEvaluator(t, p)
	base := Parse(AppSonarr, "Show.S01E01.1080p.WEB.h264-GRP")
	gb := int64(1024 * 1024 * 1024)
	torrent := base
	torrent.Protocol, torrent.Size = ProtocolTorrent, 8*gb
	usenet := base
	usenet.Protocol, usenet.Size = ProtocolUsenet, 2*gb
	a, b := ev.Evaluate(torrent), ev.Evaluate(usenet)
	if ev.Compare(b, a) >= 0 {
		t.Error("preferred protocol must win before size")
	}
	big, small := usenet, usenet
	big.Size, small.Size = 6*gb, 2*gb
	now := time.Now()
	big.PublishDate, small.PublishDate = now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour)
	if ev.Compare(ev.Evaluate(big), ev.Evaluate(small)) >= 0 {
		t.Error("without a preferred size the larger release wins")
	}
}

func TestPreferredSizeWithRuntime(t *testing.T) {
	p := testProfile()
	pref := 50.0 // MB per minute
	p.SizeLimits = map[string]SizeLimit{"WEBDL-1080p": {Min: 5, Preferred: &pref}}
	ev := newTestEvaluator(t, p)
	mb := int64(1024 * 1024)
	near := Parse(AppSonarr, "Show.S01E01.1080p.WEB.h264-A")
	far := Parse(AppSonarr, "Show.S01E01.1080p.WEB.h264-B")
	near.Size, far.Size = 2500*mb, 6000*mb // preferred = 50 MB * 50 min = 2500 MB
	ctx := ReleaseContext{RuntimeMinutes: 50}
	if ev.Compare(ev.Evaluate(ev.Prepare(near, ctx)), ev.Evaluate(ev.Prepare(far, ctx))) >= 0 {
		t.Error("closest to the preferred size should win")
	}
}

func TestSizeLimitsAreEnforcedWhenEnabled(t *testing.T) {
	p := testProfile()
	max := 10.0
	p.SizeLimits = map[string]SizeLimit{"WEBDL-1080p": {Min: 2, Max: &max}}
	p.EnforceSizeLimits = true
	ev := newTestEvaluator(t, p)
	mb := int64(1024 * 1024)
	r := Parse(AppSonarr, "Show.S01E01.1080p.WEB.h264-GRP")
	ctx := ReleaseContext{RuntimeMinutes: 60}
	r.Size = 50 * mb // below 2 MB/min * 60
	if e := ev.Evaluate(ev.Prepare(r, ctx)); e.Allowed {
		t.Error("undersized release must be rejected")
	}
	r.Size = 700 * mb // above 10 MB/min * 60
	if e := ev.Evaluate(ev.Prepare(r, ctx)); e.Allowed {
		t.Error("oversized release must be rejected")
	}
	r.Size = 300 * mb
	if e := ev.Evaluate(ev.Prepare(r, ctx)); !e.Allowed {
		t.Errorf("in-range release must pass: %+v", e.Rejections)
	}
	// Season packs scale the limits by the episode count.
	pack := Parse(AppSonarr, "Show.S01E01E02E03.1080p.WEB.h264-GRP")
	pack.Size, pack.EpisodeCount = 1500*mb, 3
	if e := ev.Evaluate(ev.Prepare(pack, ctx)); !e.Allowed {
		t.Errorf("3-episode release within 3x limits must pass: %+v", e.Rejections)
	}
	// A season pack of unknown length cannot be judged by size.
	season := Parse(AppSonarr, "Show.S01.1080p.WEB.h264-GRP")
	season.Size = 40000 * mb
	if !season.FullSeason {
		t.Fatal("expected a full season pack")
	}
	if e := ev.Evaluate(ev.Prepare(season, ctx)); !e.Allowed {
		t.Errorf("a season pack without an episode count must not be size-rejected: %+v", e.Rejections)
	}
}

func TestRadarrProfileLanguage(t *testing.T) {
	p := Profile{App: AppRadarr, Name: "French", Language: "French",
		Items:        []ProfileItem{{Name: "WEBDL-1080p", Allowed: true}},
		FormatScores: map[string]int{}}
	ev, errs := NewEvaluator(p, nil)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	en := ev.Evaluate(ev.Prepare(Parse(AppRadarr, "Movie.2020.1080p.WEB-DL.h264-G"), ReleaseContext{OriginalLanguage: "English"}))
	if en.Allowed {
		t.Error("English release must be rejected by a French profile")
	}
	fr := ev.Evaluate(ev.Prepare(Parse(AppRadarr, "Movie.2020.FRENCH.1080p.WEB-DL.h264-G"), ReleaseContext{OriginalLanguage: "English"}))
	if !fr.Allowed {
		t.Errorf("French release must pass: %v", fr.Rejections)
	}
	p.Language = LanguageOriginal
	ev, _ = NewEvaluator(p, nil)
	de := ev.Evaluate(ev.Prepare(Parse(AppRadarr, "Movie.2020.GERMAN.1080p.WEB-DL.h264-G"), ReleaseContext{OriginalLanguage: "English"}))
	if de.Allowed {
		t.Error("German-only release lacks the original language")
	}
}

func TestMultiLanguagesSetting(t *testing.T) {
	p := testProfile()
	p.MultiLanguages = []Language{"German", "English"}
	ev := newTestEvaluator(t, p)
	e := evalTitle(ev, "Show.S01E01.MULTi.1080p.WEB.h264-GRP", ReleaseContext{OriginalLanguage: "English"})
	// German + English with an English original also counts as German DL.
	if e.Score != 1500 {
		t.Errorf("MULTi should be treated as German+English, got score %d languages %v", e.Score, e.Release.Languages)
	}
}

func TestValidateProfile(t *testing.T) {
	bad := []Profile{
		{App: "kodi", Name: "x", Items: []ProfileItem{{Name: "WEBDL-1080p", Allowed: true}}},
		{App: AppSonarr, Name: "", Items: []ProfileItem{{Name: "WEBDL-1080p", Allowed: true}}},
		{App: AppSonarr, Name: "x"},
		{App: AppSonarr, Name: "x", Items: []ProfileItem{{Name: "Remux-1080p", Allowed: true}}}, // Radarr name
		{App: AppSonarr, Name: "x", Items: []ProfileItem{{Name: "WEBDL-1080p", Allowed: false}}},
		{App: AppSonarr, Name: "x", Items: []ProfileItem{{Name: "WEBDL-1080p", Allowed: true}, {Name: "WEB", Allowed: true, Qualities: []string{"WEBDL-1080p"}}}},
	}
	for i, p := range bad {
		if err := ValidateProfile(p); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
	if err := ValidateProfile(testProfile()); err != nil {
		t.Errorf("valid profile: %v", err)
	}
}
