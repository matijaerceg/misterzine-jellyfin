package ui

import (
	"context"
	"fmt"
	"plexcrt/internal/jellyfin"
	"strings"
)

func (a *App) openItem(original *jellyfin.Item, at string) {
	item := *original
	client := a.Client
	if item.Type == "movie" && item.Summary != "" && item.SourceID != "" {
		a.Push(NewPreplay(a, original))
		return
	}
	a.requestServer(func(ctx context.Context) (func(), error) {
		it := &item
		if it.Type == "movie" || it.Type == "show" || (it.Type == "episode" && (it.SeriesID == "" || it.ParentID == "")) {
			fresh, err := client.Item(ctx, it.ID)
			if err != nil {
				return nil, err
			}
			it = fresh
		}
		if it.Type == "movie" {
			return func() { a.takeFresh(original, it); a.Push(NewPreplay(a, original)) }, nil
		}
		show := it
		seasonKey, episodeKey := "", at
		if it.Type == "season" || it.Type == "episode" {
			showKey := it.ParentID
			seasonKey = it.ID
			if it.Type == "episode" {
				showKey = it.SeriesID
				seasonKey = it.ParentID
				episodeKey = it.ID
			}
			var err error
			show, err = client.Item(ctx, showKey)
			if err != nil {
				return nil, err
			}
		}
		seasons, err := client.Items(ctx, show.Key, nil, 500)
		if err != nil {
			return nil, err
		}
		filtered := seasons[:0]
		for _, s := range seasons {
			if !strings.HasSuffix(s.Key, "/allLeaves") {
				filtered = append(filtered, s)
			}
		}
		seasons = filtered
		if len(seasons) == 0 {
			return nil, fmt.Errorf("no seasons available")
		}
		si := 0
		for i, s := range seasons {
			if s.ID == seasonKey {
				si = i
			}
		}
		if it.Type == "episode" && len(it.Versions) > 1 {
			// Continue Watching names an episode by the version in progress,
			// the season by its first: open at the season's own
			if eps, err := client.Items(ctx, seasons[si].Key, nil, 500); err == nil {
				episodeKey = listedEpisode(eps, it)
			}
		}
		return func() {
			if seasonKey != "" || len(seasons) == 1 {
				a.Push(NewShowInSeason(a, show, seasons, si, episodeKey))
			} else {
				a.Push(NewShow(a, show, seasons))
			}
		}, nil
	})
}

// listedEpisode is the id a season's listing gives an episode: its own, or
// the listed one of the same number when the episode came by another
// version's (its id is then not listed).
func listedEpisode(eps []*jellyfin.Item, it *jellyfin.Item) string {
	for _, e := range eps {
		if e.ID == it.ID {
			return e.ID
		}
	}
	for _, e := range eps {
		if it.Index > 0 && e.Index == it.Index {
			return e.ID
		}
	}
	return it.ID
}

// markItem marks an item watched or not, and each of its versions: on a
// server that keeps each version's resume point, a version's own would
// otherwise stay.
func (a *App) markItem(it *jellyfin.Item, watched bool, done func()) {
	client, key := a.Client, it.ID
	ids := []string{key}
	for _, v := range it.Versions {
		if v.Detailed() && v.ID != key {
			ids = append(ids, v.ID)
		}
	}
	a.requestServer(func(ctx context.Context) (func(), error) {
		for _, id := range ids {
			if err := client.Scrobble(ctx, id, watched); err != nil {
				return nil, err
			}
		}
		fresh, err := client.Item(ctx, key)
		if err != nil {
			return nil, err
		}
		return func() {
			a.takeFresh(it, fresh)
			if watched {
				a.forgetVersion(it) // its next play is from the start
			}
			done()
		}, nil
	})
}
