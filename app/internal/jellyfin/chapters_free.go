//go:build !premium

package jellyfin

import (
	"context"
	"net/url"
	"plexcrt/internal/chapters"
)

func (x *dto) chaptersFor(index int) []chapters.Chapter                   { return nil }
func chapterFields(fields string) string                                  { return fields }
func (c *Client) loadChapterVersions(ctx context.Context, it *Item) error { return nil }
func chapterImagePath(ref string, q ImageQuery) (string, url.Values, error, bool) {
	return "", nil, nil, false
}
