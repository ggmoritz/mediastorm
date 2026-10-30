package models

import "testing"

func TestPortraitPosterRejectsSeasonBanners(t *testing.T) {
	for _, tc := range []struct {
		name  string
		image *Image
		want  bool
	}{
		{"missing", nil, false},
		{"empty", &Image{}, false},
		{"TVDB banner labeled poster", &Image{URL: "https://artworks.thetvdb.com/banners/v4/season/6527/banners/64ddac2483fe8.jpg", Type: "poster"}, false},
		{"TVDB seasonwide", &Image{URL: "https://artworks.thetvdb.com/banners/seasonwide/73838-1.jpg", Type: "poster"}, false},
		{"TVDB placeholder", &Image{URL: "https://artworks.thetvdb.com/banners/images/missing/series.jpg"}, false},
		{"landscape dimensions", &Image{URL: "https://img.example/cover.jpg", Type: "poster", Width: 400, Height: 73}, false},
		{"banner type", &Image{URL: "https://img.example/cover.jpg", Type: "banner"}, false},
		{"TVDB season poster", &Image{URL: "https://artworks.thetvdb.com/banners/v4/season/6527/posters/cover.jpg", Type: "poster"}, true},
		{"TVDB legacy season poster", &Image{URL: "https://artworks.thetvdb.com/banners/seasons/73838-1.jpg", Type: "poster"}, true},
		{"TMDB poster", &Image{URL: "https://image.tmdb.org/t/p/w500/poster.jpg", Type: "poster"}, true},
		{"portrait dimensions", &Image{URL: "https://img.example/cover.jpg", Type: "poster", Width: 400, Height: 600}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.image.IsPortraitPoster(); got != tc.want {
				t.Fatalf("IsPortraitPoster() = %v, want %v", got, tc.want)
			}
		})
	}
}
