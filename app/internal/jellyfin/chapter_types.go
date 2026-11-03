package jellyfin

// chapterDTO is the server's chapter metadata contract. Interpretation is supplied by the supporter overlay.
type chapterDTO struct {
	StartPositionTicks *int64 `json:"StartPositionTicks"`
	Name               string `json:"Name"`
	ImageTag           string `json:"ImageTag"`
}
