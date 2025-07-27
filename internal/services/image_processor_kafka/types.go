package image_processor

type DataType = string

const (
	// DataTypeImage represents an image data type
	DataTypeAnime     DataType = "Anime"
	DataTypeCharacter DataType = "Character"
	DataTypeStaff     DataType = "Staff"
)

type Payload struct {
	Data ImageSchema `json:"data"`
}

type ImageSchema struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}
