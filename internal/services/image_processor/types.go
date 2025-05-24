package image_processor

type DataType = string

const (
	// DataTypeImage represents an image data type
	DataTypeAnime     DataType = "Anime"
	DataTypeCharacter DataType = "Character"
	DataTypeStaff     DataType = "Staff"
)

type Schema struct {
	Name string   `json:"name"`
	URL  string   `json:"url"`
	Type DataType `json:"type"`
}

type Payload struct {
	Data Schema `json:"data"`
}
