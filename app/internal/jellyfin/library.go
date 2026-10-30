package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// listFields are the optional fields every listing asks for: enough for the
// poster wall, the info panel and the picture aspect, without the media
// sources (large, and only needed when an item is opened), but with their
// count (an item with versions asks which to play).
const listFields = "SortName,Overview,Taglines,Genres,ChildCount,RecursiveItemCount,ParentId,Width,Height,MediaSourceCount"

// itemFields are those of a single item: everything a pre-play page shows,
// and the scrub preview sheets the playback overlay uses.
const itemFields = listFields + ",MediaSources,MediaStreams,Trickplay"

// splitPath separates a listing's path from the query it carries
// ("/Shows/x/Episodes?seasonId=y") and merges q over it.
func splitPath(listing string, q url.Values) (string, url.Values) {
	path, raw, _ := strings.Cut(listing, "?")
	out, _ := url.ParseQuery(raw)
	if out == nil {
		out = url.Values{}
	}
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return path, out
}

// prepare fills in what every listing query needs: the user and the fields.
func (c *Client) prepare(path string, q url.Values) url.Values {
	if q.Get("userId") == "" && c.UserID() != "" {
		q.Set("userId", c.UserID())
	}
	if q.Get("fields") == "" {
		q.Set("fields", listFields)
	}
	return q
}

// Page fetches items [start, start+size) of a listing and the total count.
// A listing is a path with an optional query: an Item's Key, or one of
// SectionListing's. A page past the end is empty and keeps the total.
func (c *Client) Page(ctx context.Context, listing string, q url.Values, start, size int) ([]*Item, int, error) {
	path, q := splitPath(listing, q)
	q = c.prepare(path, q)
	q.Set("startIndex", itoa(start))
	q.Set("limit", itoa(size))
	q.Set("enableTotalRecordCount", "true")
	data, err := c.do(ctx, c.http, "GET", path, q, nil, true, 0)
	if err != nil {
		return nil, 0, err
	}
	l, err := decodeListing(data)
	if err != nil {
		return nil, 0, fmt.Errorf("GET %s: %w", path, err)
	}
	out := make([]*Item, 0, len(l.Items))
	for i := range l.Items {
		out = append(out, l.Items[i].item())
	}
	total := l.Total
	if len(out) > 0 && total < start+len(out) {
		// /Shows/{id}/Seasons ignores paging and counts what it returned
		total = start + len(out)
	}
	return out, total, nil
}

// Items fetches the first max items of a listing.
func (c *Client) Items(ctx context.Context, listing string, q url.Values, max int) ([]*Item, error) {
	items, _, err := c.Page(ctx, listing, q, 0, max)
	return items, err
}

// all walks a listing a page at a time until the total; a listing that
// never ends is refused.
func (c *Client) all(ctx context.Context, listing string, q url.Values) ([]*Item, error) {
	const page, pages = 500, 100
	var out []*Item
	for range pages {
		items, total, err := c.Page(ctx, listing, q, len(out), page)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) < page || len(out) >= total {
			return out, nil
		}
	}
	return nil, fmt.Errorf("%s: more than %d items", pathOnly(listing), page*pages)
}

// Sections lists the user's movie and show libraries (GET /UserViews).
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var l listing
	if err := c.getJSON(ctx, c.http, "/UserViews", c.userQuery(), &l); err != nil {
		return nil, err
	}
	var out []Section
	for _, v := range l.Items {
		switch v.CollectionType {
		case "movies":
			out = append(out, Section{Key: v.ID, Title: Fold(v.Name), Type: "movie"})
		case "tvshows":
			out = append(out, Section{Key: v.ID, Title: Fold(v.Name), Type: "show"})
		}
	}
	return out, nil
}

// Library views, the tabs of a library's wall.
const (
	ViewAZ          = "az"
	ViewAdded       = "added"
	ViewReleased    = "released"
	ViewContinue    = "continue"
	ViewFolders     = "folders"
	ViewCollections = "collections"
)

// SectionListing is the listing of one view of a library, for Page. kind
// is the section's Type (movie or show).
func SectionListing(section, kind, view string) (string, url.Values) {
	itemType := "Movie"
	if kind == "show" {
		itemType = "Series"
	}
	q := url.Values{"parentId": {section}, "recursive": {"true"}, "includeItemTypes": {itemType}}
	switch view {
	case ViewAdded:
		q.Set("sortBy", "DateCreated,SortName")
		q.Set("sortOrder", "Descending")
	case ViewReleased:
		q.Set("sortBy", "PremiereDate,SortName")
		q.Set("sortOrder", "Descending")
	case ViewContinue:
		// in progress within the library; a show library's are its episodes
		q = url.Values{"parentId": {section}, "mediaTypes": {"Video"}}
		return "/UserItems/Resume", q
	case ViewFolders:
		// the library's top level, not recursive: Jellyfin flattens a
		// movie library's folders, so this is a plain listing there
		return "/Items", url.Values{"parentId": {section}, "sortBy": {"IsFolder,SortName"}}
	case ViewCollections:
		// box sets are not children of a library in Jellyfin; this lists
		// them all (the UI can filter by Subtype once known)
		return "/Items", url.Values{"includeItemTypes": {"BoxSet"}, "recursive": {"true"}, "sortBy": {"SortName"}}
	default:
		q.Set("sortBy", "SortName")
		q.Set("sortOrder", "Ascending")
	}
	return "/Items", q
}

// Item fetches one item in full: media sources and streams, and its intro
// and credits markers when the server has found them. An item with
// versions on a server that keeps each one's resume point has them read,
// and its own is the first version's (versionOffsets).
func (c *Client) Item(ctx context.Context, id string) (*Item, error) {
	q := c.userQuery()
	q.Set("fields", itemFields)
	var x dto
	if err := c.getJSON(ctx, c.http, "/Items/"+url.PathEscape(id), q, &x); err != nil {
		return nil, err
	}
	if x.ID == "" {
		return nil, fmt.Errorf("item %s not found", id)
	}
	it := x.item()
	if len(it.Versions) > 1 && it.Versions[0].Detailed() && c.ownResumePoints(ctx) {
		if err := c.versionOffsets(ctx, it); errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
	}
	if it.Playable() {
		// markers are a nicety: a server without segments still plays
		if m, err := c.Markers(ctx, id); err == nil {
			it.Markers = m
		} else if errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
	}
	return it, nil
}

// Markers lists an item's intro and credits (GET /MediaSegments/{id}).
func (c *Client) Markers(ctx context.Context, id string) ([]Marker, error) {
	var l struct {
		Items []struct {
			Type       string `json:"Type"`
			StartTicks int64  `json:"StartTicks"`
			EndTicks   int64  `json:"EndTicks"`
		} `json:"Items"`
	}
	q := url.Values{"includeSegmentTypes": {"Intro", "Outro"}}
	if err := c.getJSON(ctx, c.http, "/MediaSegments/"+url.PathEscape(id), q, &l); err != nil {
		return nil, err
	}
	var out []Marker
	for _, s := range l.Items {
		kind := ""
		switch s.Type {
		case "Intro":
			kind = "intro"
		case "Outro":
			kind = "credits"
		default:
			continue
		}
		out = append(out, Marker{Type: kind, Start: float64(s.StartTicks) / TicksPerSecond, End: float64(s.EndTicks) / TicksPerSecond})
	}
	return out, nil
}

// ShowEpisodes lists every episode of a show, all its seasons and specials
// in the server's order, a page at a time.
func (c *Client) ShowEpisodes(ctx context.Context, showID string) ([]*Item, error) {
	return c.all(ctx, "/Shows/"+url.PathEscape(showID)+"/Episodes", nil)
}

// ShowAspect is the picture aspect of a show's first regular episode, 0
// when the server has not measured it: one small request per show. Specials
// (season 0) sort first and are often a different shape, so they only
// answer for a show whose first page holds nothing else.
func (c *Client) ShowAspect(ctx context.Context, showID string) (float64, error) {
	items, err := c.Items(ctx, "/Shows/"+url.PathEscape(showID)+"/Episodes", url.Values{"fields": {"Width,Height,MediaStreams"}}, 10)
	if err != nil || len(items) == 0 {
		return 0, err
	}
	for _, it := range items {
		if it.Season != 0 {
			return it.Aspect, nil
		}
	}
	return items[0].Aspect, nil
}

// FirstEpisodeAspects is the picture aspect of every show in a TV library
// with an episode 1 of some season, keyed by show id and taken from its
// lowest season other than specials (season 0, often a different shape;
// a show with only specials is left to ShowAspect): one request for the
// whole library. The streams come too:
// a DVD rip's pixels are 720x480 whatever its picture, and only the
// stream's aspect ratio says which shape the player shows.
func (c *Client) FirstEpisodeAspects(ctx context.Context, section string) (map[string]float64, error) {
	q := url.Values{"parentId": {section}, "recursive": {"true"}, "includeItemTypes": {"Episode"},
		"indexNumber": {"1"}, "fields": {"Width,Height,MediaStreams"}, "enableImages": {"false"}}
	items, err := c.all(ctx, "/Items", q)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	season := map[string]int{}
	for _, it := range items {
		if it.SeriesID == "" || it.Aspect == 0 || it.Season == 0 {
			continue
		}
		if s, seen := season[it.SeriesID]; seen && s <= it.Season {
			continue
		}
		season[it.SeriesID] = it.Season
		out[it.SeriesID] = it.Aspect
	}
	return out, nil
}

// Letter is one entry of a library's first-character index.
type Letter struct {
	Key  string // "#" or "A".."Z"
	Size int
}

// FirstChars returns the A-Z index of a library view for the alphabet
// jump. Jellyfin has no such endpoint: it is 27 counting requests (limit 0),
// so the UI should fetch it once per library and keep it.
func (c *Client) FirstChars(ctx context.Context, section, kind string) ([]Letter, error) {
	path, base := SectionListing(section, kind, ViewAZ)
	base.Set("limit", "0")
	base.Set("enableTotalRecordCount", "true")
	base.Set("enableImages", "false")
	base.Set("enableUserData", "false")
	if c.UserID() != "" {
		base.Set("userId", c.UserID())
	}
	count := func(q url.Values) (int, error) {
		var l listing
		if err := c.getJSON(ctx, c.http, path, q, &l); err != nil {
			return 0, err
		}
		return l.Total, nil
	}
	var out []Letter
	q := cloneValues(base)
	q.Set("nameLessThan", "A")
	n, err := count(q)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		out = append(out, Letter{"#", n})
	}
	for ch := 'A'; ch <= 'Z'; ch++ {
		q := cloneValues(base)
		q.Set("nameStartsWith", string(ch))
		n, err := count(q)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, Letter{string(ch), n})
		}
	}
	return out, nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// Hubs returns the home rows that are not per library: Continue Watching,
// the items in progress (GET /UserItems/Resume) followed by the next
// episodes (GET /Shows/NextUp) it does not already hold.
func (c *Client) Hubs(ctx context.Context, count int) ([]*Hub, error) {
	items, err := c.continueItems(ctx, count)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return []*Hub{{Title: "Continue Watching", Ident: "home.continue", Items: items}}, nil
}

func (c *Client) continueItems(ctx context.Context, count int) ([]*Item, error) {
	q := c.userQuery()
	q.Set("limit", itoa(count))
	q.Set("mediaTypes", "Video")
	q.Set("fields", listFields)
	var resume, next listing
	if err := c.getJSON(ctx, c.slow, "/UserItems/Resume", q, &resume); err != nil {
		return nil, err
	}
	nq := c.userQuery()
	nq.Set("limit", itoa(count))
	nq.Set("fields", listFields)
	nq.Set("enableResumable", "false")
	if err := c.getJSON(ctx, c.slow, "/Shows/NextUp", nq, &next); err != nil {
		return nil, err
	}
	var out []*Item
	seen := map[string]bool{}
	for _, l := range []*listing{&resume, &next} {
		for i := range l.Items {
			it := l.Items[i].item()
			if !seen[it.ID] {
				seen[it.ID] = true
				out = append(out, it)
			}
		}
	}
	return out, nil
}

// SectionRecent returns a library's recently added row (GET /Items/Latest),
// or nil when it is empty. A show library's row groups new episodes by
// show or season, as the server does.
func (c *Client) SectionRecent(ctx context.Context, section string, count int) (*Hub, error) {
	q := c.userQuery()
	q.Set("parentId", section)
	q.Set("limit", itoa(count))
	q.Set("fields", listFields)
	data, err := c.do(ctx, c.slow, "GET", "/Items/Latest", q, nil, true, 0)
	if err != nil {
		return nil, err
	}
	l, err := decodeListing(data)
	if err != nil {
		return nil, fmt.Errorf("GET /Items/Latest: %w", err)
	}
	if len(l.Items) == 0 {
		return nil, nil
	}
	hub := &Hub{Title: "Recently Added", Ident: "home.recent." + section}
	for i := range l.Items {
		hub.Items = append(hub.Items, l.Items[i].item())
	}
	return hub, nil
}

// ContinueWatching returns the ids in Continue Watching (in progress and
// next up) without the home row's cap.
func (c *Client) ContinueWatching(ctx context.Context) (map[string]bool, error) {
	items, err := c.continueItems(ctx, 500)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, it := range items {
		keys[it.ID] = true
	}
	return keys, nil
}

// ClearResumePoint forgets how far an item was watched (POST
// /UserItems/{id}/UserData), which also takes it out of Continue Watching.
// Unlike Plex, Jellyfin has no "hide" that keeps the point. A next-up
// episode has no point to clear and stays.
func (c *Client) ClearResumePoint(ctx context.Context, id string) error {
	return c.post(ctx, "/UserItems/"+url.PathEscape(id)+"/UserData", c.userQuery(),
		map[string]any{"PlaybackPositionTicks": 0}, nil)
}

// Scrobble marks an item watched (true) or unwatched (false): POST or
// DELETE /UserPlayedItems/{id}.
func (c *Client) Scrobble(ctx context.Context, id string, watched bool) error {
	path := "/UserPlayedItems/" + url.PathEscape(id)
	if watched {
		return c.post(ctx, path, c.userQuery(), nil, nil)
	}
	return c.del(ctx, path, c.userQuery())
}

// Search returns movie and show matches for a query (GET /Items?searchTerm=).
func (c *Client) Search(ctx context.Context, query string) ([]*Item, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	q := url.Values{"searchTerm": {query}, "recursive": {"true"}, "includeItemTypes": {"Movie,Series"}}
	return c.Items(ctx, "/Items", q, 30)
}

// parseIndex reads a stream id back as Jellyfin's stream index.
func parseIndex(id string) (int, bool) {
	n, err := strconv.Atoi(id)
	return n, err == nil && n >= 0
}
