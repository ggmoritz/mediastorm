package indexer

import (
	"encoding/xml"
	"testing"
)

// Newznab/Torznab attributes are namespaced elements; they used to be
// dropped because the struct tags named the prefix ("newznab:attr").
func TestFeedAttributesAreParsed(t *testing.T) {
	const doc = `<rss xmlns:newznab="http://www.newznab.com/DTD/2010/feeds/attributes/" xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item><title>A</title><newznab:attr name="size" value="123"/><newznab:attr name="language" value="German"/><torznab:attr name="seeders" value="5"/></item></channel></rss>`
	var feed struct {
		Channel struct {
			Items []rssItem `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(doc), &feed); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range feed.Channel.Items[0].Attrs {
		got[a.Name] = a.Value
	}
	if got["size"] != "123" || got["language"] != "German" || got["seeders"] != "5" {
		t.Fatalf("attributes not parsed: %v", got)
	}
}

// Attributes whose names mediastorm uses itself are prefixed: a newznab
// category ("5040") must not replace the download client category.
func TestFeedAttributesKeepInternalKeysFree(t *testing.T) {
	item := rssItem{Attrs: []torznabAttr{
		{Name: "category", Value: "5000"}, {Name: "category", Value: "5040"},
		{Name: "resolution", Value: "1920x1080"}, {Name: "group", Value: "alt.binaries.hdtv"},
		{Name: "Size", Value: "42"}, {Name: "seeders", Value: "3"},
	}}
	got := feedAttributes(item)
	for _, key := range []string{"category", "resolution", "group"} {
		if _, ok := got[key]; ok {
			t.Errorf("%q must not be set from the feed: %v", key, got)
		}
	}
	want := map[string]string{"indexerCategory": "5040", "indexerResolution": "1920x1080", "newsgroup": "alt.binaries.hdtv", "size": "42", "seeders": "3"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}
