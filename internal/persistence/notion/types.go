package notion

type Page struct {
	Parent     Parent         `json:"parent"`
	Properties map[string]any `json:"properties"`
	Children   []Block        `json:"children,omitempty"`
}

// Block represents a Notion block for page body content.
type Block struct {
	Object           string            `json:"object"`
	Type             string            `json:"type"`
	Heading3         *RichTextBlock    `json:"heading_3,omitempty"`
	BulletedListItem *RichTextBlock    `json:"bulleted_list_item,omitempty"`
}

// RichTextBlock is a block that contains rich text content.
type RichTextBlock struct {
	RichText []RichText `json:"rich_text"`
}

type Parent struct {
	DatabaseID string `json:"database_id"`
}

type QueryDatabaseRequest struct {
	Filter      any    `json:"filter,omitempty"`
	Sorts       []Sort `json:"sorts,omitempty"`
	StartCursor string `json:"start_cursor,omitempty"`
	PageSize    int    `json:"page_size,omitempty"`
}

type Sort struct {
	Property  string `json:"property"`
	Direction string `json:"direction"`
}

type UpdatePageRequest struct {
	Properties map[string]any `json:"properties,omitempty"`
	Archived   bool           `json:"archived,omitempty"`
}

// Property Helpers

type TitleProperty struct {
	Title []RichText `json:"title"`
}

type RichText struct {
	Text Text `json:"text"`
}

type Text struct {
	Content string `json:"content"`
}

type NumberProperty struct {
	Number float64 `json:"number"`
}

type SelectProperty struct {
	Select SelectOption `json:"select"`
}

type SelectOption struct {
	Name string `json:"name"`
}

type DateProperty struct {
	Date DateInfo `json:"date"`
}

type DateInfo struct {
	Start string `json:"start"`
}

type PeopleProperty struct {
	People []Person `json:"people"`
}

type Person struct {
	ID string `json:"id"`
}

type Filter struct {
	Property string        `json:"property,omitempty"`
	Date     *DateFilter   `json:"date,omitempty"`
	People   *PeopleFilter `json:"people,omitempty"`
	And      []Filter      `json:"and,omitempty"`
}

type DateFilter struct {
	OnOrAfter  string `json:"on_or_after,omitempty"`
	OnOrBefore string `json:"on_or_before,omitempty"`
}

type PeopleFilter struct {
	Contains string `json:"contains,omitempty"`
}

type QueryResponse struct {
	Results    []PageObject `json:"results"`
	HasMore    bool         `json:"has_more"`
	NextCursor *string      `json:"next_cursor"`
}

type PageObject struct {
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties"`
}

// File upload types

type FileUploadResponse struct {
	ID string `json:"id"`
}

type FilesProperty struct {
	Files []FileReference `json:"files"`
}

type FileReference struct {
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	FileUpload *FileUploadRef `json:"file_upload,omitempty"`
}

type FileUploadRef struct {
	ID string `json:"id"`
}
