package models

import (
	"net/url"
	"path"
	"strings"
)

// IsPortraitPoster rejects landscape artwork mislabeled as a poster by a
// provider. TVDB's season.image can point to a wide banner without dimensions.
func (image *Image) IsPortraitPoster() bool {
	if image == nil || strings.TrimSpace(image.URL) == "" {
		return false
	}
	if image.Width > 0 && image.Height > 0 && image.Width >= image.Height {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(image.Type)) {
	case "banner", "backdrop", "background", "fanart", "thumb", "still", "logo":
		return false
	}
	u, err := url.Parse(image.URL)
	if err != nil {
		return false
	}
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".thetvdb.com") {
		p := strings.ToLower(u.Path)
		if strings.Contains(p, "/seasonwide/") || strings.Contains(p, "/seasonswide/") ||
			strings.Contains(p, "/fanart/") || strings.Contains(p, "/backgrounds/") ||
			strings.Contains(p, "/images/missing/") ||
			(strings.Contains(p, "/season/") && path.Base(path.Dir(p)) == "banners") {
			return false
		}
	}
	return true
}
