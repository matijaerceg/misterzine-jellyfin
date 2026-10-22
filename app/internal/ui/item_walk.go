package ui

import "plexcrt/internal/jellyfin"

// forEachItem calls fn once for every in-memory copy of the item with this
// id: those the screens on the stack hold (rows, the wall's listing, a
// show's seasons, a season's episodes, a page's own item, search results,
// the drawer's list) and the shared season listing. A playback and a
// refresh from the server change all of them, so whichever screen the user
// returns to shows the same resume point. Render thread only.
func (a *App) forEachItem(id string, fn func(*jellyfin.Item)) {
	if id == "" {
		return
	}
	seen := map[*jellyfin.Item]bool{}
	visit := func(it *jellyfin.Item) {
		if it != nil && it.ID == id && !seen[it] {
			seen[it] = true
			fn(it)
		}
	}
	each := func(items []*jellyfin.Item) {
		for _, it := range items {
			visit(it)
		}
	}
	screens := map[Screen]bool{}
	var walk func(s Screen)
	walk = func(s Screen) {
		if s == nil || screens[s] {
			return
		}
		screens[s] = true
		switch v := s.(type) {
		case *Home:
			if v == nil {
				return
			}
			visit(v.fixed)
			for _, h := range v.hubs {
				if h != nil {
					each(h.Items)
				}
			}
			if v.view != nil {
				walk(v.view)
			}
		case *Show:
			if v == nil {
				return
			}
			visit(v.item)
			each(v.seasons)
			if v.picker != nil {
				walk(v.picker)
			}
			if v.season != nil {
				walk(v.season)
			}
		case *Season:
			if v == nil {
				return
			}
			visit(v.show)
			each(v.seasons)
			each(v.eps)
			if v.view != nil {
				walk(v.view)
			}
		case *Preplay:
			if v != nil {
				visit(v.item)
			}
		case *Search:
			if v != nil {
				each(v.items)
			}
		case *Drawer:
			if v != nil {
				each(v.items)
				walk(v.under)
			}
		case *Wall:
			if v != nil {
				visit(v.coll)
				each(v.pager.copies())
			}
		}
	}
	for _, s := range a.stack {
		walk(s)
	}
	if a.seasonData != nil {
		each(a.seasonData.eps)
	}
}

// copies is every item the pager holds, loaded pages and kept lists alike.
func (p *Pager) copies() []*jellyfin.Item {
	var out []*jellyfin.Item
	for ; p != nil; p = p.base {
		p.mu.Lock()
		for _, it := range p.items {
			out = append(out, it)
		}
		out = append(out, p.list...)
		out = append(out, p.stage...)
		p.mu.Unlock()
	}
	return out
}

// copyWatchState gives dst src's watch state: resume point and watched marks.
func copyWatchState(dst, src *jellyfin.Item) {
	dst.ViewOffset, dst.ViewCount, dst.Viewed, dst.Played = src.ViewOffset, src.ViewCount, src.Viewed, src.Played
}

// notePlayed sets the played item's resume point, in every copy, to where
// the playback stopped: the position its stopped report gives the server.
// The server's own record (refreshPlayedItem) follows once that report is
// in; until then a Resume would otherwise start from the old point. A
// playback that ran to the end (finished, or past the 90 % at which the
// server marks an item played) clears the point and marks it watched.
func (a *App) notePlayed(it *jellyfin.Item, sess *Session, finished bool) {
	pos, ok := sess.FinalPosition()
	if !ok {
		return // never started: the server keeps the old point
	}
	done := finished || (it.Duration > 0 && pos >= 0.9*float64(it.Duration))
	if done {
		it.ViewOffset, it.Played = 0, true
		it.ViewCount++ // as the server counts a play
	} else {
		it.ViewOffset = int(pos)
	}
	a.forEachItem(it.ID, func(c *jellyfin.Item) {
		if c != it {
			copyWatchState(c, it)
		}
	})
}

// afterReports runs fn on the render thread once the latest playback's
// stopped report has been sent, so what fn asks the server reflects it.
func (a *App) afterReports(fn func()) {
	reports := a.playReports
	if reports == nil {
		fn()
		return
	}
	select {
	case <-reports:
		fn()
		return
	default:
	}
	go func() {
		<-reports
		a.Later(fn)
	}()
}
