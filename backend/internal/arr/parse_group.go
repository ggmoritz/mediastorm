package arr

import (
	"regexp"
	"strings"
)

var (
	// Website prefixes such as "[www.site.com] - " or "www.site.de - ".
	reSitePrefix = regexp.MustCompile(`(?i)^\s*(?:\[\s*(?:www\.[a-z0-9-]+(?:\.[a-z0-9-]+)+|[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:com|net|org|info|co\.uk|co\.nz|de|to|io|me|tv|cc|nl))\s*\]|www\.[a-z0-9-]+(?:\.[a-z0-9-]+)+)\s*(?:-\s*)?`)
	// Noise after a "-GROUP" tag: language words, subtitle notes, a dangling
	// single letter, parenthesized notes or a path suffix.
	reGroupNoise  = regexp.MustCompile(`(?i)(?:[._ ](?:english|icelandic|nlsubs?|d|ac3\.\(nlsub\))|\s+\([^()]*\)|\\.*)$`)
	reDoubleTail  = regexp.MustCompile(`-([A-Za-z0-9]+)-([A-Za-z0-9]+)$`)
	reBracketAny  = regexp.MustCompile(`\[([^\]]+)\]`)
	reBracketTags = regexp.MustCompile(`(?i)^(?:\d{3,4}p|blu-?ray|bd|web(?:-?dl|rip)?|hdtv|dvd(?:rip)?|mkv|mp4|avi|h\.?26[45]|x26[45]|hevc|avc|aac.*|ac3.*|flac.*|dts.*|dual[- ]?audio|multi(?:-?subs?)?|eng|jap|jpn|vostfr|softsubs.*|hardsubs?|subbed|dubbed|remux|uhd|hdr|sdr|10bit|8bit|hi10p?|batch|complete|end|v\d|s\d+|season\s?\d+|\d+)$`)
	// Groups named after their site, e.g. "-YTS.MX".
	reDomainGroup = regexp.MustCompile(`(?i)-([a-z0-9]+\.(?:mx|lt|ag|am|to))$`)
	// Trailing tracker/indexer tags: "[rarbg]", "[eztv]-[rarbg.com]", "[QxR]" ...
	reTrailingSiteTag = regexp.MustCompile(`(?i)(?:[-\s]*\[(?:rarbg(?:\.com)?|rartv|eztv(?:\.re)?|ettv|ethd|tgx|torrentgalaxy|publichd|rarbg\.to|vtv|qxr|shogho|n-z-b|www\.[^\]]+)\])+\s*$`)
	// Repost/indexer suffixes appended after the real group.
	reRepostSuffix  = regexp.MustCompile(`(?i)-(?:rp|obfuscated|scrambled|postbot|xpost|nzbgeek|asrequested|alternativetorequested|buymore|whiterev|chamele0n|pre|gerov|rakuv[a-z0-9]*|z0ids3n|4planet|4p|altezachen|repackpost|sample|1)$`)
	reGroupTail     = regexp.MustCompile(`-([^\s\-.\[\]()]+)$`)
	reBracketTail   = regexp.MustCompile(`\[([^\]\s]+)\]$`)
	reBracketHead   = regexp.MustCompile(`^\s*(?:\[([^\]]+)\]|【([^】]+)】)`)
	reParenTail     = regexp.MustCompile(`\(([^()]*)\)$`)
	reDashSpaceTail = regexp.MustCompile(`\s-\s\[?([A-Za-z0-9][A-Za-z0-9_.]*)\]?$`)
	reHexHash       = regexp.MustCompile(`^[0-9A-Fa-f]{8}$`)
	reAnimeStyle    = regexp.MustCompile(`^\s*(?:\[[^\]]+\]|【[^】]+】)`)
)

// Groups whose names contain separators that would otherwise split them.
var knownMultiTokenGroups = []string{
	"D-Z0N3", "Anime-Koi", "Erai-raws", "Fight-BB", "BEN THE MEN", "BEN.THE.MEN", "E.N.D",
}

// Tokens that end a title but are never a group.
var notGroups = map[string]bool{
	"dl": true, "rip": true, "ray": true, "hd": true, "tv": true, "sd": true, "web": true, "x264": true, "x265": true,
	"h264": true, "h265": true, "264": true, "265": true, "hevc": true, "avc": true, "mux": true, "audio": true,
	"sub": true, "subs": true, "dts": true, "aac": true, "ac3": true, "mkv": true, "mp4": true, "avi": true,
}

func trimGroupNoise(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimRight(t, "'\"")
	for i := 0; i < 4; i++ {
		next := strings.TrimSpace(reTrailingSiteTag.ReplaceAllString(t, ""))
		next = strings.TrimSpace(reRepostSuffix.ReplaceAllString(next, ""))
		if next == t {
			break
		}
		t = next
	}
	return t
}

func parseReleaseGroup(app App, title string) string {
	t := stripExtension(strings.TrimSpace(title))
	t = reSitePrefix.ReplaceAllString(t, "")
	t = trimGroupNoise(t)
	if t == "" {
		return ""
	}
	lower := strings.ToLower(t)
	for _, g := range knownMultiTokenGroups {
		lg := strings.ToLower(g)
		for _, sep := range []string{"-", ".", " ", " - "} {
			if strings.HasSuffix(lower, sep+lg) {
				return t[len(t)-len(g):]
			}
		}
	}
	// A variant without trailing notes is used for "-GROUP" detection only;
	// bracket and parenthesis rules below look at the unmodified title.
	tagged := t
	for i := 0; i < 3; i++ {
		next := strings.TrimSpace(reGroupNoise.ReplaceAllString(tagged, ""))
		if next == tagged {
			break
		}
		tagged = next
	}
	tagged = strings.TrimRight(tagged, "-")
	if strings.HasSuffix(strings.ToLower(t), " qxr") {
		t = strings.TrimSpace(t[:len(t)-4])
	}
	// Anime naming: "[Group] Title - 01 [1080p]".
	if reAnimeStyle.MatchString(t) {
		if m := reBracketHead.FindStringSubmatch(t); m != nil {
			g := strings.TrimSpace(m[1] + m[2])
			if g != "" && !reHexHash.MatchString(g) {
				return g
			}
		}
	}
	// "-A-B" endings are reported as one hyphenated group ("x264-ACC-ROLLED")
	// unless the first part is a format tag ("WEB-DL-Group").
	if m := reDoubleTail.FindStringSubmatch(tagged); m != nil {
		a, b := m[1], m[2]
		if !notGroups[strings.ToLower(a)] && !strings.EqualFold(a, "remux") && !notGroups[strings.ToLower(b)] && !isDigits(b) {
			return a + "-" + b
		}
	}
	if m := reDomainGroup.FindStringSubmatch(tagged); m != nil {
		return m[1]
	}
	if m := reGroupTail.FindStringSubmatch(tagged); m != nil {
		g := m[1]
		// "GROUP_English" / "GROUP_CreW": the group ends at the underscore.
		if i := strings.Index(g, "_"); i > 0 {
			g = g[:i]
		}
		if !notGroups[strings.ToLower(g)] && (!isDigits(g) || (app == AppRadarr && len(g) >= 4)) {
			return g
		}
	}
	// "Title 1999 [Group] [Blu-ray][MKV][h264][1080p]..." style.
	for _, m := range reBracketAny.FindAllStringSubmatch(t, -1) {
		if app != AppRadarr {
			break
		}
		g := strings.TrimSpace(m[1])
		if g != "" && !strings.ContainsAny(g, " .") && !reHexHash.MatchString(g) && !isBracketTag(g) {
			if strings.Index(t, "["+m[1]+"]") > 0 {
				return g
			}
		}
		break
	}
	if m := reBracketTail.FindStringSubmatch(t); m != nil {
		g := m[1]
		if !reHexHash.MatchString(g) && !notGroups[strings.ToLower(g)] && !strings.EqualFold(g, "pack") {
			return g
		}
	}
	if m := reParenTail.FindStringSubmatch(t); m != nil {
		inner := strings.TrimSpace(m[1])
		if i := strings.LastIndex(inner, " - "); i >= 0 {
			return strings.TrimSpace(inner[i+3:])
		}
		fields := strings.Fields(inner)
		if len(fields) >= 3 {
			last := fields[len(fields)-1]
			if !notGroups[strings.ToLower(last)] && !isDigits(last) && reVideoTagged.MatchString(inner) {
				return last
			}
		}
	}
	if m := reDashSpaceTail.FindStringSubmatch(t); m != nil {
		g := m[1]
		if !notGroups[strings.ToLower(g)] && !isDigits(g) && parseResolution(t) > 0 {
			return g
		}
	}
	return ""
}

var reVideoTagged = regexp.MustCompile(`(?i)\b(?:\d{3,4}p|x26[45]|h\.?26[45]|hevc|avc|web-?dl|webrip|bluray|hdtv)\b`)

func isDigits(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isBracketTag(s string) bool {
	return reBracketTags.MatchString(strings.TrimSpace(s)) || parseResolution(s) > 0
}
