package image_processor

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

/*
MyAnimeList serves two copies of every image under the same name: the
225px thumbnail the scraper records, and a larger one (about 424x600 for a
poster) with an `l` before the extension:

	https://cdn.myanimelist.net/images/anime/1668/108792.jpg   225x318
	https://cdn.myanimelist.net/images/anime/1668/108792l.jpg  424x600

The larger one is what gets stored when it exists. Nearly twice the
resolution at the source beats anything an upscaler can invent, and text on
a poster is legible at 424px where it was not at 225.
*/

// malHosts are the hosts the variant rule applies to. A var so a test can
// point it at a local server.
var malHosts = map[string]bool{"cdn.myanimelist.net": true}

var malImage = regexp.MustCompile(`^(/images/.+/\d+)(\.(?:jpe?g|png|webp))$`)

// largerVariant names the `l` copy of a MyAnimeList image URL, or "" when
// the URL is not one (or already is the large copy).
func largerVariant(src string) string {
	u, err := url.Parse(src)
	if err != nil || !malHosts[strings.ToLower(u.Host)] {
		return ""
	}
	m := malImage.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	u.Path = m[1] + "l" + m[2]
	return u.String()
}

// preferredSource is the URL to fetch: the larger copy when the host has
// one, else the URL as given. One HEAD per image; a miss falls back.
func preferredSource(ctx context.Context, src string) string {
	large := largerVariant(src)
	if large == "" {
		return src
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, large, nil)
	if err != nil {
		return src
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return src
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return src
	}
	return large
}
