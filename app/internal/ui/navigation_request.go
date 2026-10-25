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
		return func() {
			if seasonKey != "" || len(seasons) == 1 {
				a.Push(NewShowInSeason(a, show, seasons, si, episodeKey))
			} else {
				a.Push(NewShow(a, show, seasons))
			}
		}, nil
	})
}

func (a *App) markItem(it *jellyfin.Item, watched bool, done func()) {
	client, key := a.Client, it.ID
	a.requestServer(func(ctx context.Context) (func(), error) {
		if err := client.Scrobble(ctx, key, watched); err != nil {
			return nil, err
		}
		fresh, err := client.Item(ctx, key)
		if err != nil {
			return nil, err
		}
		return func() { a.takeFresh(it, fresh); done() }, nil
	})
}
