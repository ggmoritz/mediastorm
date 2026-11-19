package arr

import (
	"regexp"
	"strings"
)

type languageRule struct {
	lang Language
	re   *regexp.Regexp
}

// lw builds a case-insensitive whole-word rule.
func lw(lang Language, pattern string) languageRule {
	return languageRule{lang: lang, re: regexp.MustCompile(`(?i)\b(?:` + pattern + `)\b`)}
}

// lraw builds a rule from a raw pattern (case-sensitive unless it opts in).
func lraw(lang Language, pattern string) languageRule {
	return languageRule{lang: lang, re: regexp.MustCompile(pattern)}
}

// languageRules detect audio languages in release names. Short codes are
// only recognized where scene and tracker naming uses them unambiguously;
// codes such as "Ger", "Fre", "Por" or "Jpn" are deliberately not treated as
// languages because *arr does not treat them as such either.
var languageRules = []languageRule{
	lw(LanguageEnglish, `english|eng|ing|ingles|inglés`),
	lw("French", `french|truefrench|fran[cç]ais|vf|fr`),
	lw("Spanish", `spanish|espa[nñ]ol|castellano|esp|spa`),
	lw(LanguageGerman, `german|deutsch|swissgerman|ger[-. ]?dub`),
	lw("Italian", `italian|italiano|ita`),
	lw("Danish", `danish`),
	lw("Dutch", `dutch|nl`),
	lw("Flemish", `flemish`),
	lw("Japanese", `japanese`),
	lw("Icelandic", `icelandic`),
	lw("Chinese", `chinese|mandarin|cantonese`),
	lraw("Chinese", `(?i)\[(?:chs|cht|gb|big5)\]|[简繁]|中日`),
	lw("Russian", `russian|rus|ru`),
	lw("Polish", `polish|pl|dubpl|lekpl|pllek|plsub|pldub`),
	lw("Vietnamese", `vietnamese`),
	lw("Swedish", `swedish|swesub`),
	lw("Norwegian", `norwegian|norsk`),
	lw("Finnish", `finnish`),
	lw("Turkish", `turkish|tur`),
	lw("Portuguese", `portuguese`),
	lw("Greek", `greek`),
	lw("Korean", `korean`),
	lw("Hungarian", `hungarian|hundub|hun`),
	lw("Hebrew", `hebrew|hebdub|heb`),
	lw("Lithuanian", `lithuanian|lt`),
	lw("Czech", `czech|cz`),
	lw("Arabic", `arabic`),
	lw("Hindi", `hindi`),
	lw("Bulgarian", `bulgarian|bg[-. ]?audio`),
	lw("Malayalam", `malayalam`),
	lraw("Ukrainian", `(?i)(?:\b|\d|\dx)(?:ukrainian|ukr)\b`),
	lw("Slovak", `slovak|sk`),
	lw("Thai", `thai`),
	lw("Portuguese (Brazil)", `brazilian|dublado|pt[-. ]?br`),
	lw("Spanish (Latino)", `latino|latam`),
	lw("Romanian", `romanian|rodubbed`),
	lw("Latvian", `latvian|lav|lv|lat`),
	lw("Persian", `persian|farsi`),
	lw("Catalan", `catalan|catal[aá]n|cat`),
	lw("Croatian", `croatian`),
	lw("Serbian", `serbian`),
	lw("Bosnian", `bosnian`),
	lw("Estonian", `estonian`),
	lw("Tamil", `tamil`),
	lw("Indonesian", `indonesian`),
	lw("Macedonian", `macedonian`),
	lw("Slovenian", `slovenian`),
	lw("Telugu", `telugu`),
	lw("Bengali", `bengali`),
}

var (
	// "DL" (dual language) marks German releases that also carry the
	// original audio. The "DL" of "WEB-DL" does not count.
	reDualLanguageDL = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])dl(?:$|[^a-z0-9])`)
	reWebDLSuffix    = regexp.MustCompile(`(?i)web[-. ]?dl(?:$|[^a-z0-9])`)
	reMultiLangML    = regexp.MustCompile(`(?i)\bml\b`)
	reOriginalTag    = regexp.MustCompile(`(?i)\b(?:orig|original)\b`)
)

func hasDualLanguageTag(tokens string) bool {
	stripped := reWebDLSuffix.ReplaceAllString(tokens, " ")
	return reDualLanguageDL.MatchString(stripped)
}

// radarrLanguageRules are Radarr's broader language tags: Radarr also knows
// codes such as "Fre", "Ger", "Jap", "Kor", "EN", "ES", "DE" and many full
// names, but not "ESP", "ING", "CAT" or a bare "PL"/"NL".
var radarrLanguageRules = []languageRule{
	lw(LanguageEnglish, `english|eng`),
	lraw(LanguageEnglish, `(?:^|[^A-Za-z0-9])EN(?:$|[^A-Za-z0-9])`),
	lw("French", `french|truefrench|fran[cç]ais|vff|vfq|vfi|vf2|vostfr|vf|fre|fra|fr`),
	lw("Spanish", `spanish|espa[nñ]ol|castellano|spa`),
	lraw("Spanish", `(?:^|[^A-Za-z0-9-])ES(?:$|[^A-Za-z0-9])`),
	lw(LanguageGerman, `german|deutsch|swissgerman|ger|de|ger[-. ]?dub`),
	lw("Italian", `italian|italiano|ita`),
	lw("Danish", `danish|dan`),
	lw("Dutch", `dutch`),
	lw("Flemish", `flemish`),
	lw("Japanese", `japanese|jap`),
	lw("Icelandic", `icelandic|ice`),
	lw("Chinese", `chinese|mandarin|cantonese|chi`),
	lraw("Chinese", `(?i)\[(?:chs|cht|gb|big5)\]|[简繁]|中日`),
	lw("Russian", `russian|rus|ru`),
	lw("Polish", `polish|pl[-. ]?dub|dub[-. ]?pl|pl[-. ]?lek|lek[-. ]?pl`),
	lraw("Polish", `\.PL\.`),
	lw("Vietnamese", `vietnamese|vie`),
	lw("Swedish", `swedish|swe|swesub`),
	lw("Norwegian", `norwegian|norsk|nor`),
	lw("Finnish", `finnish|fin`),
	lw("Turkish", `turkish|tur`),
	lw("Portuguese", `portuguese|por`),
	lw("Greek", `greek|gre`),
	lw("Korean", `korean|kor`),
	lw("Hungarian", `hungarian|hundub|hun`),
	lw("Hebrew", `hebrew|hebdub|hebdubbed|heb`),
	lw("Lithuanian", `lithuanian|lt`),
	lw("Czech", `czech|cze|cz`),
	lw("Arabic", `arabic|ara`),
	lw("Hindi", `hindi`),
	lw("Bulgarian", `bulgarian|bg[-. ]?audio`),
	lw("Malayalam", `malayalam`),
	lraw("Ukrainian", `(?i)(?:\b|\d|\dx)(?:ukrainian|ukr)\b`),
	lw("Slovak", `slovak|sk`),
	lw("Thai", `thai`),
	lw("Portuguese (Brazil)", `brazilian|dublado|pt[-. ]?br`),
	lw("Spanish (Latino)", `latino|latam`),
	lw("Romanian", `romanian|rodubbed|rom`),
	lw("Latvian", `latvian|lav|lv|lat`),
	lw("Persian", `persian|farsi`),
	lw("Catalan", `catalan|catal[aàá]`),
	lw("Croatian", `croatian`),
	lw("Serbian", `serbian`),
	lw("Bosnian", `bosnian`),
	lw("Estonian", `estonian`),
	lw("Tamil", `tamil`),
	lw("Indonesian", `indonesian`),
	lw("Macedonian", `macedonian`),
	lw("Slovenian", `slovenian`),
	lw("Telugu", `telugu|tel`),
	lw("Bengali", `bengali`),
	lw("Kannada", `kannada`),
	lw("Albanian", `albanian`),
	lw("Afrikaans", `afrikaans`),
	lw("Marathi", `marathi`),
	lw("Tagalog", `tagalog`),
	lw("Urdu", `urdu`),
	lw("Romansh", `romansh|romansch|rumantsch`),
	lw("Mongolian", `mongolian|khalkha`),
	lw("Georgian", `georgian|geo`),
}

var reYTSDomain = regexp.MustCompile(`(?i)yts\.(?:lt|mx|am|ag)`)

// parseLanguagesFor detects languages with the app's rule set. Radarr scans
// the whole title except its first word (always part of the movie name).
func parseLanguagesFor(app App, title, tokens string) []Language {
	if app != AppRadarr {
		return parseLanguages(tokens)
	}
	scan := reYTSDomain.ReplaceAllString(title, " ")
	if i := strings.IndexAny(scan, ". _"); i > 0 {
		scan = scan[i:]
	}
	return detectLanguages(radarrLanguageRules, scan)
}

func parseLanguages(tokens string) []Language {
	return detectLanguages(languageRules, tokens)
}

func detectLanguages(rules []languageRule, tokens string) []Language {
	// Underscores stay word characters, so "_ENG_" is not a language tag.
	normalized := tokens
	var out []Language
	for _, rule := range rules {
		if rule.re.MatchString(normalized) && !containsLanguage(out, rule.lang) {
			out = append(out, rule.lang)
		}
	}
	german := containsLanguage(out, LanguageGerman)
	if german && reMultiLangML.MatchString(normalized) {
		if !containsLanguage(out, LanguageEnglish) {
			out = append(out, LanguageEnglish)
		}
		out = append(out, LanguageOriginal)
	} else if german && len(out) == 1 && hasDualLanguageTag(normalized) {
		out = append(out, LanguageOriginal)
	}
	if reOriginalTag.MatchString(normalized) && !containsLanguage(out, LanguageOriginal) {
		out = append(out, LanguageOriginal)
	}
	if len(out) == 0 {
		out = []Language{LanguageUnknown}
	}
	return out
}

// reMultiTag marks releases that carry several audio languages ("MULTi").
var reMultiTag = regexp.MustCompile(`(?i)\bmulti\b`)
