package image_processor

import "github.com/weeb-vip/image-sync/internal/services/imagepath"

type DataType = string

const (
	// DataTypeImage represents an image data type
	DataTypeAnime     DataType = imagepath.TypeAnime
	DataTypeCharacter DataType = imagepath.TypeCharacter
	DataTypeStaff     DataType = imagepath.TypeStaff
	DataTypeBanner    DataType = imagepath.TypeBanner
	DataTypePoster    DataType = imagepath.TypePoster
)

type Payload struct {
	Data ImageSchema `json:"data"`
}

type ImageSchema struct {
	// ID is what the object is keyed by. Name is kept only so messages
	// published before producers sent ids still land somewhere sensible.
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}
