// Package imagepath owns the one rule that decides where an image lands in the
// bucket, so the pulsar and kafka processors cannot drift apart.
package imagepath

import "net/url"

const (
	TypeAnime     = "Anime"
	TypeCharacter = "Character"
	TypeStaff     = "Staff"
	TypeBanner    = "Banner"
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
	default:
		return "", false
	}
}
