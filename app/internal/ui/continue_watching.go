package ui

import (
	"context"
	"maps"
	"time"

	"plexcrt/internal/jellyfin"
)

// ForgetProgress is the last action on a movie's page and in an episode's
// actions while the item is in Continue Watching with a resume point.
// Jellyfin cannot hide an item and keep its place, so the action says what
// it does: the resume point is cleared and the item leaves the row. A next
// up episode has no point to clear and does not get it.
const ForgetProgress = "Forget my progress"

// continueList is the server's Continue Watching list as rating keys, held
// by a movie or season page. The page fetches it off-thread when it opens,
// after a play or a mark, and every viewRefreshInterval after that (a
// change made on another client, a fetch that failed), and takes the
// answer on the render thread (pollRefresh). Until an answer lands, or
// when the server has no list, nothing counts as in it and the action
// stays hidden.
type continueList struct {
	keys    map[string]bool
	pending chan map[string]bool // a nil answer: the fetch failed
	next    time.Time            // when to ask again; zero: never asked
}

// refetch asks the server again; an answer still on its way is dropped.
func (l *continueList) refetch(a *App) {
	if a.Client == nil {
		return
	}
	l.next = time.Now().Add(viewRefreshInterval)
	result := make(chan map[string]bool, 1)
	l.pending = result
	client, lg := a.Client, a.Log
	go func() {
		keys, err := client.ContinueWatching(bg())
		if err != nil && lg != nil {
			lg.Printf("continue watching: %v", err)
		}
		result <- keys
		select {
		case a.Wake <- struct{}{}:
		default:
		}
	}()
}

// poll takes an answer that has landed, or asks again when it is time;
// true when the list changed.
func (l *continueList) poll(a *App, now time.Time) bool {
	if l.pending == nil {
		if !l.next.IsZero() && !now.Before(l.next) {
			l.refetch(a)
		}
		return false
	}
	select {
	case keys := <-l.pending:
		l.pending = nil
		if keys == nil || maps.Equal(keys, l.keys) {
			return false
		}
		l.keys = keys
		return true
	default:
		return false
	}
}

func (l *continueList) has(key string) bool { return l.keys[key] }

// forgetProgress clears an item's resume point on the server, which takes
// it out of Continue Watching, then out of the page's list and the Home row
// at once, not at their next fetch. The request screen reports failures.
func (a *App) forgetProgress(it *jellyfin.Item, l *continueList, done func()) {
	client, key := a.Client, it.ID
	a.requestServer(func(ctx context.Context) (func(), error) {
		if err := client.ClearResumePoint(ctx, key); err != nil {
			return nil, err
		}
		return func() {
			it.ViewOffset = 0
			l.pending = nil // an answer from before the removal would still hold it
			delete(l.keys, it.ID)
			for _, screen := range a.stack {
				if h, ok := screen.(*Home); ok && h.home {
					h.dropContinue(it.ID)
				}
			}
			a.dirty = true
			done()
		}, nil
	})
}

// dropContinue takes an item out of the Continue Watching row and has the
// rows fetched again when Home is next shown, discarding any fetch that
// began before the removal.
func (h *Home) dropContinue(key string) {
	h.refreshResult = nil
	h.refreshAt = time.Time{}
	hubs := make([]*jellyfin.Hub, 0, len(h.hubs))
	found := false
	for _, hub := range h.hubs {
		cp := *hub
		if hub.IsContinueWatching() {
			cp.Items = nil
			for _, it := range hub.Items {
				if it.ID == key {
					found = true
					continue
				}
				cp.Items = append(cp.Items, it)
			}
		}
		hubs = append(hubs, &cp)
	}
	if found {
		h.applyHome(hubs)
	}
}
