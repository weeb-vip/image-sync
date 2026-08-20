// Package imagepath owns the one rule that decides where an image lands in the
// bucket, so the pulsar and kafka processors cannot drift apart.
package imagepath

import "net/url"

const (
	TypeAnime     = "Anime"
	TypeCharacter = "Character"
	TypeStaff     = "Staff"
	TypeBanner    = "Banner"
	// TypePoster is the high-resolution series poster from TheTVDB (~680x1000),
	// which is NOT the same thing as the image at the bucket root. The root
	// object under an anime's id is the scraper's MyAnimeList image, stored at
	// MAL's own 225px thumbnail size -- fine for a list row, soft the moment it
	// fills a phone hero. Both are 2:3 "posters" in the everyday sense, so the
	// distinction is: root = whatever the scraper had, /posters/ = the good one.
	TypePoster = "Poster"
)

// For builds the object path for an image record.
//
// Records are keyed by their own id. Name is only a fallback for messages
// published before the producers started sending ids — in-flight messages
// during a rolling deploy, and the retry topic. Returns false for a type we do
// not store, or when there is nothing to key on.
func For(dataType, id, name string) (string, bool) {
	key := id
	if key == "" {
		key = name
	}
	if key == "" {
		return "", false
	}
	// A no-op for ids, which are uuids; still applied so a fallback name lands
	// under exactly the key it used to.
	key = url.QueryEscape(key)

	switch dataType {
	case TypeAnime:
		return "/" + key, true
	case TypeCharacter:
		return "/characters/" + key, true
	case TypeStaff:
		return "/staff/" + key, true
	case TypeBanner:
		return "/banners/" + key, true
	case TypePoster:
		return "/posters/" + key, true
	default:
		return "", false
	}
}
