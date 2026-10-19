package ui

import (
	"testing"

	"plexcrt/internal/jellyfin"
)

func TestHeading(t *testing.T) {
	cases := []struct {
		item       jellyfin.Item
		title, sub string
	}{
		{jellyfin.Item{Type: "movie", Title: "Star Voyager", Year: 1984}, "Star Voyager", "1984"},
		{jellyfin.Item{Type: "movie", Title: "Undated"}, "Undated", ""},
		{jellyfin.Item{Type: "episode", Title: "Pilot", SeriesTitle: "Starlight Stories", Season: 1, Index: 4, Year: 1992}, "Starlight Stories", "S1 E4  Pilot"},
	}
	for _, c := range cases {
		it := c.item
		title, sub := heading(&it)
		if title != c.title || sub != c.sub {
			t.Errorf("%s: got %q / %q, want %q / %q", c.item.Title, title, sub, c.title, c.sub)
		}
	}
}
