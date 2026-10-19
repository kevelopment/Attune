package model

type Book struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Authors       []string          `json:"authors"`
	Subjects      []string          `json:"subjects"`
	Publisher     string            `json:"publisher,omitempty"`
	Published     string            `json:"published,omitempty"`
	Identifiers   map[string]string `json:"identifiers,omitempty"`
	Description   string            `json:"description,omitempty"`
	Series        string            `json:"series,omitempty"`
	SeriesIndex   string            `json:"seriesIndex,omitempty"`
	AuthorSort    []string          `json:"authorSort,omitempty"`
	TitleSort     string            `json:"titleSort,omitempty"`
	PageCount     int               `json:"pageCount,omitempty"`
	FileSize      int64             `json:"fileSize,omitempty"`
	ModifiedAt    string            `json:"modifiedAt,omitempty"`
	Language      string            `json:"language"`
	SHA256        string            `json:"sha256"`
	CoverURL      string            `json:"coverURL"`
	AddedAt       string            `json:"addedAt"`
	ImportBatchID string            `json:"importBatchID,omitempty"`
	Revision      int               `json:"revision"`
}

// BookMetadataEdit replaces the editable descriptive metadata while leaving
// identifiers, page count, and other source-derived fields intact.
type BookMetadataEdit struct {
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	Publisher   string   `json:"publisher"`
	Language    string   `json:"language"`
	Published   string   `json:"published"`
	Subjects    []string `json:"subjects"`
	Series      string   `json:"series"`
	SeriesIndex string   `json:"seriesIndex"`
	Description string   `json:"description"`
}

type PreviewChapter struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href"`
}

type BookPreview struct {
	Book     Book             `json:"book"`
	Chapters []PreviewChapter `json:"chapters"`
}

type Device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Root       string `json:"root"`
	FreeBytes  uint64 `json:"freeBytes"`
	TotalBytes uint64 `json:"totalBytes"`
}

type DeviceBook struct {
	Path        string            `json:"path"`
	Name        string            `json:"name"`
	Size        int64             `json:"size"`
	BookID      string            `json:"bookID"`
	Title       string            `json:"title"`
	Authors     []string          `json:"authors"`
	Language    string            `json:"language,omitempty"`
	Subjects    []string          `json:"subjects,omitempty"`
	CoverURL    string            `json:"coverURL,omitempty"`
	Publisher   string            `json:"publisher,omitempty"`
	Published   string            `json:"published,omitempty"`
	Identifiers map[string]string `json:"identifiers,omitempty"`
	Description string            `json:"description,omitempty"`
	Series      string            `json:"series,omitempty"`
	SeriesIndex string            `json:"seriesIndex,omitempty"`
	PageCount   int               `json:"pageCount,omitempty"`
	AuthorSort  []string          `json:"authorSort,omitempty"`
	TitleSort   string            `json:"titleSort,omitempty"`
	ModifiedAt  string            `json:"modifiedAt,omitempty"`
	AddedAt     string            `json:"addedAt,omitempty"`
}

type Conversion struct {
	BookID string
	Key    string
	Path   string
}

type Progress struct {
	ID      string `json:"id"`
	BookID  string `json:"bookID"`
	Title   string `json:"title"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

type ImportResult struct {
	Imported   int      `json:"imported"`
	Duplicates int      `json:"duplicates"`
	Skipped    int      `json:"skipped,omitempty"`
	BookIDs    []string `json:"bookIDs"`
	Errors     []string `json:"errors"`
}

type OnboardingState struct {
	Required  bool                     `json:"required"`
	Libraries []CalibreLibraryLocation `json:"libraries"`
}

type CalibreLibraryLocation struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type ImportProgress struct {
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Filename   string `json:"filename"`
	Imported   int    `json:"imported"`
	Duplicates int    `json:"duplicates"`
	Skipped    int    `json:"skipped,omitempty"`
	Failed     int    `json:"failed"`
	Stage      string `json:"stage"`
}

type AppState struct {
	Books            []Book   `json:"books"`
	Devices          []Device `json:"devices"`
	ConverterReady   bool     `json:"converterReady"`
	ConverterVersion string   `json:"converterVersion"`
	LibraryPath      string   `json:"libraryPath"`
}
