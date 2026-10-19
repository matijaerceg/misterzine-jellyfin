package jellyfin

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Image types an image ref can name; ImageTags and BackdropImageTags of an
// item decide which exist.
const (
	ImagePrimary  = "Primary"
	ImageBackdrop = "Backdrop"
	ImageThumb    = "Thumb"
	ImageLogo     = "Logo"
)

// ParseImageRef splits an image ref ("id/Type/tag") into its parts.
func ParseImageRef(ref string) (id, kind, tag string, ok bool) {
	parts := strings.Split(ref, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// ImageQuery is how a picture is sized and encoded by the server.
type ImageQuery struct {
	FillWidth, FillHeight int // cover the box, cropping (posters, backdrops)
	MaxWidth, MaxHeight   int // fit inside the box, never upscaled (logos)
	Quality               int // JPEG/WebP quality, 0 for the server's
	Format                string
}

// ImagePath is the server path and query of a picture: GET
// /Items/{id}/Images/{type}?tag=...; the tag makes the URL change when the
// picture does, so a cached copy never goes stale. No token: images are
// served to anyone who knows the id.
func ImagePath(ref string, q ImageQuery) (string, url.Values, error) {
	id, kind, tag, ok := ParseImageRef(ref)
	if !ok {
		return "", nil, errors.New("no art")
	}
	v := url.Values{"tag": {tag}}
	set := func(k string, n int) {
		if n > 0 {
			v.Set(k, strconv.Itoa(n))
		}
	}
	set("fillWidth", q.FillWidth)
	set("fillHeight", q.FillHeight)
	set("maxWidth", q.MaxWidth)
	set("maxHeight", q.MaxHeight)
	set("quality", q.Quality)
	if q.Format != "" {
		v.Set("format", q.Format)
	}
	return "/Items/" + url.PathEscape(id) + "/Images/" + url.PathEscape(kind), v, nil
}

// ImageURL is the absolute URL of a picture.
func (c *Client) ImageURL(ref string, q ImageQuery) (string, error) {
	path, v, err := ImagePath(ref, q)
	if err != nil {
		return "", err
	}
	return c.URL(path, v), nil
}

func artQuery(w, h int, logo bool) (ImageQuery, string) {
	if logo {
		return ImageQuery{MaxWidth: w, MaxHeight: h, Format: "Png"}, ".png"
	}
	return ImageQuery{FillWidth: w, FillHeight: h, Quality: 90, Format: "Jpg"}, ".jpg"
}

// Art fetches a poster or backdrop at w x h, cropped by the server to cover
// the box, cached on disk as JPEG. Returns the file path.
func (c *Client) Art(ctx context.Context, ref string, w, h int) (string, error) {
	return c.picture(ctx, ref, w, h, false)
}

// Logo fetches a clear logo as a PNG with alpha that fits inside w x h,
// cached on disk. Returns the file path.
func (c *Client) Logo(ctx context.Context, ref string, w, h int) (string, error) {
	return c.picture(ctx, ref, w, h, true)
}

// ArtData is Art without writing to the disk cache.
func (c *Client) ArtData(ctx context.Context, ref string, w, h int) ([]byte, error) {
	return c.pictureData(ctx, ref, w, h, false)
}

// LogoData is Logo without writing to the disk cache.
func (c *Client) LogoData(ctx context.Context, ref string, w, h int) ([]byte, error) {
	return c.pictureData(ctx, ref, w, h, true)
}

func (c *Client) pictureSource(ref string, w, h int, logo bool) (string, string, url.Values, error) {
	q, ext := artQuery(w, h, logo)
	path, v, err := ImagePath(ref, q)
	if err != nil {
		return "", "", nil, err
	}
	// the same ref on another server must not share cached bytes; the
	// tag in the ref already changes with the picture
	sum := sha1.Sum([]byte(fmt.Sprintf("%s\x00%s|%dx%d", c.Host, ref, w, h)))
	return filepath.Join(c.CacheDir, hex.EncodeToString(sum[:8])+ext), path, v, nil
}

const pictureLimit = 8 << 20

func (c *Client) pictureData(ctx context.Context, ref string, w, h int, logo bool) ([]byte, error) {
	file, path, v, err := c.pictureSource(ref, w, h, logo)
	if err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(file); err == nil {
		return data, nil
	}
	return c.do(ctx, c.http, "GET", path, v, nil, true, pictureLimit)
}

func (c *Client) picture(ctx context.Context, ref string, w, h int, logo bool) (string, error) {
	file, path, v, err := c.pictureSource(ref, w, h, logo)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	data, err := c.do(ctx, c.http, "GET", path, v, nil, true, pictureLimit)
	if err != nil {
		return "", err
	}
	return file, writeAtomic(c.CacheDir, file, data)
}

func writeAtomic(dir, file string, data []byte) error {
	f, err := os.CreateTemp(dir, ".part-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ThemeFile fetches a show's theme song (GET /Items/{id}/ThemeSongs, then
// the song's audio as stored) to the cache and returns the file path. song
// is the theme's item id when already known (Item.Theme), else "".
func (c *Client) ThemeFile(ctx context.Context, showID, song string) (string, error) {
	if song == "" {
		var l listing
		q := c.userQuery()
		q.Set("inheritFromParent", "true")
		if err := c.getJSON(ctx, c.http, "/Items/"+url.PathEscape(showID)+"/ThemeSongs", q, &l); err != nil {
			return "", err
		}
		if len(l.Items) == 0 {
			return "", errors.New("no theme")
		}
		song = l.Items[0].ID
	}
	sum := sha256.Sum256([]byte(c.Host + "|" + song))
	file := filepath.Join(c.CacheDir, "theme-"+hex.EncodeToString(sum[:]))
	if st, err := os.Stat(file); err == nil && st.Size() > 0 {
		return file, nil
	}
	const limit = 16 << 20
	data, err := c.do(ctx, c.http, "GET", "/Audio/"+url.PathEscape(song)+"/stream", url.Values{"static": {"true"}}, nil, true, limit)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", errors.New("empty theme")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return file, writeAtomic(c.CacheDir, file, data)
}
