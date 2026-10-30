package ui

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"plexcrt/internal/jellyfin"
)

// Aspects remembers the picture shape of shows, taken from each show's
// first episode, so the 4:3 filter can judge a show the way it judges a
// movie. The first time the filter needs them, every TV library is swept
// in one request each; the few shows that leaves out are probed one by one.
// Answers are kept on disk in the cache folder. Until a show is known it
// stays visible. Once a day, while the menus sit idle, the libraries are
// swept again so a replaced first episode is noticed.
type Aspects struct {
	mu         sync.Mutex
	known      map[string]float64 // show rating key -> aspect (0: seen, unknown)
	sweptAt    time.Time          // when a library sweep last finished
	path       string
	swept      chan struct{} // closed when the library sweep has finished; nil before it starts
	resweeping bool          // a periodic re-sweep is running
	retried    time.Time     // when the last re-sweep started (a failed one waits aspectRetry)
}

// aspectsFile is aspects2.json on disk.
type aspectsFile struct {
	Swept int64              `json:"swept"` // unix seconds; 0 before the first sweep
	Shows map[string]float64 `json:"shows"`
}

const (
	aspectProbes  = 8                       // shows probed at once
	aspectBudget  = 6 * time.Second         // per batch; the rest stay visible until next time
	aspectWait    = 1500 * time.Millisecond // how long a listing waits for the sweep before showing
	aspectResweep = 24 * time.Hour          // how old a sweep may get before the idle menus redo it
	aspectRetry   = time.Hour               // between re-sweeps that did not finish
)

func (a *Aspects) load(dir string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// aspects2.json: the old file judged some shows by a special, so every
	// install measures once more
	a.path = filepath.Join(dir, "aspects2.json")
	_ = os.Remove(filepath.Join(dir, "aspects.json"))
	a.known = map[string]float64{}
	a.sweptAt = time.Time{}
	a.swept = nil
	a.resweeping = false
	a.retried = time.Time{}
	var f aspectsFile
	if data, err := os.ReadFile(a.path); err == nil && json.Unmarshal(data, &f) == nil {
		if f.Shows != nil {
			a.known = f.Shows
		}
		if f.Swept > 0 {
			a.sweptAt = time.Unix(f.Swept, 0)
		}
	}
}

func (a *Aspects) save() {
	a.mu.Lock()
	f := aspectsFile{Shows: a.known}
	if !a.sweptAt.IsZero() {
		f.Swept = a.sweptAt.Unix()
	}
	data, err := json.Marshal(f)
	path := a.path
	a.mu.Unlock()
	if err != nil || path == "" {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (a *Aspects) set(key string, v float64) {
	a.mu.Lock()
	a.known[key] = v
	a.mu.Unlock()
}

// aspectShowKey gives shows and their season entries the same measurement.
func aspectShowKey(it *jellyfin.Item) string {
	switch it.Type {
	case "show":
		return it.ID
	case "season":
		if it.SeriesID != "" {
			return it.SeriesID
		}
		return it.ParentID
	}
	return ""
}

// apply fills in known show and season aspects without any request.
func (a *Aspects) apply(items []*jellyfin.Item) (unknown []*jellyfin.Item) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, it := range items {
		key := aspectShowKey(it)
		if key == "" || it.Aspect != 0 {
			continue
		}
		if v, ok := a.known[key]; ok {
			it.Aspect = v
		} else {
			unknown = append(unknown, it)
		}
	}
	return unknown
}

// sweep starts the one-off library sweep if it has not started, and
// returns the channel closed when it is done; then is called on completion.
func (a *Aspects) sweep(client *jellyfin.Client, then func()) <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.swept != nil {
		return a.swept
	}
	done := make(chan struct{})
	a.swept = done
	go func() {
		defer close(done)
		defer then()
		a.sweepLibraries(client)
	}()
	return done
}

// resweep starts a periodic library sweep if the last one is older than
// aspectResweep and no sweep is running; then is called on completion.
// Shows the sweep answers for are replaced; the probed rest are kept.
func (a *Aspects) resweep(client *jellyfin.Client, now time.Time, then func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.resweeping || a.path == "" || now.Sub(a.sweptAt) < aspectResweep || now.Sub(a.retried) < aspectRetry {
		return
	}
	if a.swept != nil {
		select {
		case <-a.swept:
		default:
			return // the first sweep is still running
		}
	}
	a.resweeping = true
	a.retried = now
	go func() {
		defer then()
		a.sweepLibraries(client)
		a.mu.Lock()
		a.resweeping = false
		a.mu.Unlock()
	}()
}

// sweepLibraries asks every TV library for its shows' first episodes,
// records the answers and the time, and saves.
func (a *Aspects) sweepLibraries(client *jellyfin.Client) {
	secs, err := client.Sections(bg())
	if err != nil {
		return
	}
	for _, s := range secs {
		if s.Type != "show" {
			continue
		}
		found, err := client.FirstEpisodeAspects(bg(), s.Key)
		if err != nil {
			continue
		}
		a.mu.Lock()
		for key, v := range found {
			a.known[key] = v
		}
		a.mu.Unlock()
	}
	a.mu.Lock()
	a.sweptAt = time.Now()
	a.mu.Unlock()
	a.save()
}

// resweepAspects redoes the library sweep once it is a day old. Cheap
// enough to call on every idle tick of the menus.
func (a *App) resweepAspects(now time.Time) {
	if a.Client == nil || a.Cfg == nil || !a.Cfg.FourThree {
		return
	}
	a.aspects.resweep(a.Client, now, func() { a.Later(a.refilterHome) })
}

// measureShows fills in the aspect of every show in items: from the
// library sweep first, then by probing what it missed (a few at a time,
// within a time budget). Safe from any goroutine; meant for the workers
// that fetch listings.
func (a *App) measureShows(items []*jellyfin.Item) {
	if a.Client == nil || a.Cfg == nil || !a.Cfg.FourThree {
		return
	}
	client := a.Client
	deadline := time.Now().Add(aspectBudget)
	if unknown := a.aspects.apply(items); len(unknown) == 0 {
		return
	}
	// a cold cache: give the sweep a moment, then show what is known and
	// let the sweep's completion refilter the home rows
	select {
	case <-a.aspects.sweep(client, func() { a.Later(a.refilterHome) }):
	case <-time.After(aspectWait):
		a.aspects.apply(items)
		return
	}
	unknown := a.aspects.apply(items)
	if len(unknown) == 0 {
		return
	}
	sem := make(chan struct{}, aspectProbes)
	var wg sync.WaitGroup
	for _, it := range unknown {
		if time.Now().After(deadline) {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it *jellyfin.Item) {
			defer wg.Done()
			defer func() { <-sem }()
			key := aspectShowKey(it)
			v, err := client.ShowAspect(bg(), key)
			if err != nil {
				return // left unknown: visible now, probed again next time
			}
			a.aspects.set(key, v)
			it.Aspect = v
		}(it)
	}
	wg.Wait()
	a.aspects.save()
}

// refilterHome asks the home screen to fetch its rows again, now that the
// sweep can judge the shows in them.
func (a *App) refilterHome() {
	if len(a.stack) > 0 {
		if h, ok := a.stack[0].(*Home); ok {
			h.refreshAt = time.Time{}
		}
	}
	a.pagers = map[string]*Pager{} // library views opened next start from known shapes
}

// measureHubs runs measureShows over every row of a home fetch.
func (a *App) measureHubs(hubs []*jellyfin.Hub) {
	var items []*jellyfin.Item
	for _, hub := range hubs {
		items = append(items, hub.Items...)
	}
	a.measureShows(items)
}

// collectionsPager is a collections listing. Under the 4:3 filter each
// page's collections are looked into (a few at a time, within a time
// budget) before they show, and those holding nothing the filter keeps
// are left out; the listing is kept for the session like any other, so
// that is once per listing. A collection that could not be looked into
// (an error, the budget spent) stays visible.
func (a *App) collectionsPager(path string, q url.Values, fourThree bool) *Pager {
	if !fourThree {
		return a.pager(path, q, nil)
	}
	var mu sync.Mutex
	empty := map[string]bool{} // collection ID -> the filter keeps none of its items
	keep := func(it *jellyfin.Item) bool {
		mu.Lock()
		defer mu.Unlock()
		return !empty[it.ID]
	}
	look := func(items []*jellyfin.Item) {
		deadline := time.Now().Add(aspectBudget)
		sem := make(chan struct{}, aspectProbes)
		var wg sync.WaitGroup
		for _, it := range items {
			if it.Type != "collection" || it.Key == "" || time.Now().After(deadline) {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(it *jellyfin.Item) {
				defer wg.Done()
				defer func() { <-sem }()
				if a.filtersEmpty(it, deadline) {
					mu.Lock()
					empty[it.ID] = true
					mu.Unlock()
				}
			}(it)
		}
		wg.Wait()
	}
	return a.preparedPager(path, q, keep, look)
}

// filtersEmpty reports whether the 4:3 filter keeps none of a collection's
// items, reading its listing until one is kept. Shows take only the shapes
// already known, without a probe: one not yet measured keeps its collection.
func (a *App) filtersEmpty(coll *jellyfin.Item, deadline time.Time) bool {
	for start := 0; time.Now().Before(deadline); {
		items, total, err := a.Client.Page(bg(), coll.Key, nil, start, pageSize)
		if err != nil {
			return false
		}
		a.aspects.apply(items)
		for _, it := range items {
			if a.Keep(it) {
				return false
			}
		}
		start += len(items)
		if len(items) == 0 || start >= total {
			return true
		}
	}
	return false
}
