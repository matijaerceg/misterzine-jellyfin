# Jellyfin API layer (`app/internal/jellyfin`)

Step 3 of `HANDOFF.md`: a Go package that replaces `app/internal/plex` for the
UI. The UI still imports `plex`; this document is the contract for the later
swap. Captured against Jellyfin 12.2.0 on 2026-10-15.

## 1. What the UI consumes from `internal/plex` today

`internal/plex` is imported by 61 files under `app/internal/ui` (27 of them
non-test) and by `cmd/plexcrt/main.go`. Counted references in those files:

| Symbol | Refs | Used for |
|---|---:|---|
| `plex.Item` | 281 | everything listed or opened |
| `plex.Section` | 43 | libraries (`Key`, `Title`, `Type` movie/show) |
| `plex.Hub` | 38 | home rows (`Title`, `Key`, `Ident`, `Items`, `IsContinueWatching`) |
| `plex.Client` | 30 | held by `App.Plex`, `Art`, `Pager`, `seasonData`, `Theme`, `Aspects` |
| `plex.New` | 21 | `New(host, token, cacheDir, clientID)` (main.go, app.go, tests) |
| `plex.Stream` | 21 | audio/subtitle pickers (`ID`, `Title`, `Selected`) |
| `plex.Server`, `Servers`, `Reach`, `LocalNameBlocked` | 14 | connect.go: plex.tv server list and probing |
| `plex.HomeUser`, `HomeUsers`, `SwitchHomeUser`, `AccountHomeIdentity` | 9 | home_users.go: Plex Home |
| `plex.Pin`, `NewPin`, `CheckPin`, `AccountName` | 6 | login.go / options.go: plex.tv link |
| `plex.Letter` | 5 | alphabet jump (`Key`, `Size`) |
| `plex.Marker` | 3 | skip intro / credits |
| `plex.Fold`, `plex.FolderListing` | 3 | text folding, folder views |

Client methods called by the UI (non-test call sites):

| Method | Sites | Where |
|---|---:|---|
| `Item(ratingKey)` | 13 | refresh after play, pre-play, season, wall, view_refresh |
| `Items(path, q, max)` | 5 | children of a show/season/collection (app.go, season_data.go, view_refresh.go) |
| `Sections()` | 4 | app.go, home.go, aspects.go |
| `Scrobble(key, watched)` | 3 | preplay.go, season.go |
| `Page(path, q, start, size)` | 1 | pager.go (every poster wall) |
| `Hubs(30)`, `SectionRecent(key, 30)` | 2 | home.go |
| `ContinueWatching()`, `RemoveFromContinueWatching(key)` | 2 | continue_watching.go |
| `Search(ctx, q)` | 1 | search.go |
| `Art`, `Logo`, `ArtData`, `LogoData` | 4 | art.go |
| `ThemeFile(ctx, key, path)` | 1 | theme.go |
| `ShowEpisodes`, `ShowAspect`, `FirstEpisodeAspects` | 3 | app.go (shuffle), aspects.go |
| `FirstChars(section, q)` | 1 | wall.go |
| `SelectAudio`, `SelectSubtitle` | 2 | app.go (stream pickers) |
| fields `Host`, `Token`, `ClientID`, `CacheDir` | ~10 | config, launcher, art cache keys |

`Item` fields read by the UI, by frequency: `Key` 66, `RatingKey` 57, `Title`
47, `Type` 36, `ViewOffset` 22, `Thumb` 16, `Subs` 16, `Audio` 15, `Duration`
15, `Year` 13, `Logo` 12, `ViewCount` 11, `PartID` 9, `Leaves` 9, `Summary` 8,
`Markers` 8, `Aspect` 8, `GrandTitle` 5, `Index` 5, `Children` 4, then
`Colors`, `Viewed`, `Art`, `GrandKey`, `ParentKey`, `Parent`, `Subtype`,
`Library` (3 each), `Still`, `Genres` (2), `Theme` (1). Methods: `Playable`,
`IsFolder`, `Label`, `SortLabel`.

**Errors and auth expiry today:** the plex client returns plain
`fmt.Errorf("%s: HTTP %d")` errors; there is no typed 401, so an expired
token surfaces as a generic load failure and the user has to sign out by
hand. Listings that fail show an empty/partial wall (`home_partial_test.go`).

**Plex paths the UI builds itself** (must change at the swap): `wall.go`
`wallViews` (`/library/sections/%s/all?sort=...`, `/hubs/sections/%s/continueWatching/items`,
`.../folder`, `.../collections`), the collection fallback
`/library/collections/{key}/children`, and `plex.FolderListing`.

## 2. The new package's surface

Same shapes, Plex-only parts dropped, renamed where the Plex name lied.
Every method that does I/O takes a `context.Context` first.

```go
c := jellyfin.New(host, cacheDir, deviceID, store) // store: TokenStore (nil = memory)
c.SignIn(ctx, user, pw)            // -> Credentials{UserID, UserName, Token, ServerID}
c.QuickConnectEnabled / QuickConnectStart / QuickConnectCheck / QuickConnectFinish
c.PublicInfo(ctx)  c.Me(ctx)  c.SignOut(ctx)  jellyfin.NormalizeHost(typed)

c.Sections(ctx) []Section
c.Page(ctx, listing, q, start, size) ([]*Item, total, error)
c.Items(ctx, listing, q, max)
jellyfin.SectionListing(sectionKey, sectionType, view) (listing, q) // replaces wallViews paths
c.Item(ctx, id)            // full: media sources, streams, markers
c.Seasons / ShowEpisodes / ShowAspect / FirstEpisodeAspects / FirstChars
c.Hubs(ctx, n)  c.SectionRecent(ctx, key, n)  c.ContinueWatching(ctx)  c.RemoveFromContinueWatching(ctx, id)
c.Scrobble(ctx, id, watched)  c.Search(ctx, q)
c.Art / Logo / ArtData / LogoData (ctx, ref, w, h)  c.ImageURL(ref, ImageQuery)  c.ThemeFile(ctx, showID, songID)
c.PlaybackInfo(ctx, id, PlaybackRequest)  c.StreamURL / DirectStreamURL
c.ReportStart / ReportProgress / ReportStopped(ctx, PlayState)
c.AuthHeader()  // for the launcher: same header on the stream request
jellyfin.ErrUnauthorized, *jellyfin.HTTPError, Fold, LangName, Ticks, Seconds
```

### Item field renames (for `gofmt -r` at the swap)

| plex | jellyfin | note |
|---|---|---|
| `RatingKey` | `ID` | Jellyfin item id (32 hex) |
| `GrandTitle` | `SeriesTitle` | |
| `GrandKey` | `SeriesID` | |
| `ParentKey` | `ParentID` | season id for an episode, show id for a season |
| `Parent` | `Season` | season number of an episode |
| `PartID` | `SourceID` | media source id, passed to PlaybackInfo |
| `Key` | `Key` | still "the children listing"; now a Jellyfin path+query that `Items`/`Page` accept |
| `Thumb`, `Art`, `Logo`, `Still` | same | now opaque **image refs** `itemId/Type/tag`, not server paths |
| `Theme` | `Theme` | theme song item id (usually empty; `ThemeFile` looks it up) |
| `Colors` | `Colors` | decoded from the poster's BlurHash corners (Jellyfin has no UltraBlur) |
| - | `Played` | new: the server's watched flag |

`Type` values are kept: `movie`, `show`, `season`, `episode`, `collection`,
plus `folder` (so `IsFolder()` is `Type == "folder"`). `Section.Type` is
`movie` or `show`; Jellyfin `movies` and `tvshows` views map to them, other
collection types (music, mixed, homevideos, ...) are skipped as the plex
client skipped music and photos. `Hub.Ident` is `home.continue` or
`home.recent.<library id>`.

### Errors

* `401` -> an `*HTTPError` for which `errors.Is(err, ErrUnauthorized)` holds.
  Not retried. The UI should drop to sign-in on it (the swap adds that check
  where loads fail; `SignIn` with a wrong password is also `ErrUnauthorized`).
* Other non-2xx -> `*HTTPError{Method, Path, Status}`. The message holds only
  the path, never the query (Quick Connect secrets) or the token.
* GET and DELETE are retried on network errors and 5xx: `Retries` (2) more
  attempts, `Backoff` 250 ms then x3, aborting on context cancellation.
  POSTs (sign-in, reports, played state) are never retried.
* Timeouts: 10 s per request, 30 s for the home rows (as in plex).
* `Logf` (optional) gets retry notices only: method and path, no query.

## 3. Jellyfin mapping

### Authorization

Every request, including images and the stream fetch:

```
Authorization: MediaBrowser Client="MisterZine", Device="MiSTer CRT", DeviceId="<stable id>", Version="0.1", Token="<token>"
```

`Token="..."` is added once signed in. `DeviceId` must be stable per install
(Jellyfin keys sessions and Quick Connect on it); the plex client id serves.
The token is never put in a URL: Jellyfin 12 refuses `?api_key=` on the
`/socket` websocket (REST still accepts it, but URLs end up in logs and in
`ps`). `AuthHeader()` is exported so the playback launcher sends the same.

### Endpoint map

Jellyfin 12's OpenAPI document (`/api-docs/openapi.json`) no longer lists the
old `/Users/{userId}/...` routes. They still answer (verified), but the
package uses the documented ones, passing `userId` as a query parameter:

| Purpose | Request | Was (older docs) |
|---|---|---|
| Server probe | `GET /System/Info/Public` (no auth) | |
| Password sign-in | `POST /Users/AuthenticateByName` `{"Username","Pw"}` -> `{User{Id,Name}, AccessToken, ServerId}` | |
| Quick Connect on? | `GET /QuickConnect/Enabled` -> `true` | |
| Quick Connect | `POST /QuickConnect/Initiate` -> `{Code, Secret}`; poll `GET /QuickConnect/Connect?secret=` until `Authenticated`; `POST /Users/AuthenticateWithQuickConnect {"Secret"}` | |
| Who am I | `GET /Users/Me` | |
| Sign out | `POST /Sessions/Logout` (revokes the token) | |
| Libraries | `GET /UserViews?userId=` | `/Users/{id}/Views` |
| Listings | `GET /Items?userId=&parentId=&includeItemTypes=&recursive=&sortBy=&sortOrder=&fields=&startIndex=&limit=&enableTotalRecordCount=true` | `/Users/{id}/Items` |
| One item | `GET /Items/{id}?userId=&fields=` | `/Users/{id}/Items/{id}` |
| Seasons | `GET /Shows/{id}/Seasons?userId=` | |
| Episodes | `GET /Shows/{id}/Episodes?userId=[&seasonId=]&startIndex=&limit=` | |
| In progress | `GET /UserItems/Resume?userId=&mediaTypes=Video` | `/Users/{id}/Items/Resume` |
| Next up | `GET /Shows/NextUp?userId=&enableResumable=false` | |
| Recently added | `GET /Items/Latest?userId=&parentId=&limit=` (bare JSON array) | `/Users/{id}/Items/Latest` |
| Search | `GET /Items?searchTerm=&includeItemTypes=Movie,Series&recursive=true` | |
| Intro/credits | `GET /MediaSegments/{id}?includeSegmentTypes=Intro&includeSegmentTypes=Outro` | |
| Theme | `GET /Items/{id}/ThemeSongs?inheritFromParent=true`, then `GET /Audio/{songId}/stream?static=true` | |
| Watched | `POST` / `DELETE /UserPlayedItems/{id}?userId=` | `/Users/{id}/PlayedItems/{id}` |
| Clear resume | `POST /UserItems/{id}/UserData?userId=` `{"PlaybackPositionTicks":0}` | |
| Images | `GET /Items/{id}/Images/{Primary,Backdrop,Thumb,Logo}?tag=&fillWidth=&fillHeight=&quality=` (posters) or `maxWidth=&maxHeight=&format=Png` (logos) | |
| Play decision | `POST /Items/{id}/PlaybackInfo?userId=` `{UserId, DeviceProfile, MaxStreamingBitrate, StartTimeTicks, AudioStreamIndex, SubtitleStreamIndex, MediaSourceId}` | |
| Reports | `POST /Sessions/Playing`, `/Sessions/Playing/Progress`, `/Sessions/Playing/Stopped` with `ItemId, MediaSourceId, PlaySessionId, PositionTicks[, IsPaused, PlayMethod, AudioStreamIndex, SubtitleStreamIndex]` | |

Ticks are 100 ns (`TicksPerSecond = 10_000_000`); the package converts at the
edge, so the UI keeps seconds everywhere (`Duration`, `ViewOffset`, markers).

### Listings and paging

A **listing** is a path with an optional query (`/Shows/{id}/Episodes?seasonId=x`).
`Page` merges the caller's query over it and adds `userId`, `fields`
(`SortName,Overview,Taglines,Genres,ChildCount,RecursiveItemCount,ParentId,Width,Height`),
`startIndex`, `limit` and `enableTotalRecordCount=true`. It reads
`{Items, TotalRecordCount}` or a bare array. A page past the end is empty and
keeps the total, which the pager relies on. `/Shows/{id}/Seasons` ignores paging;
its total is what it returned. Whole-listing walks (`ShowEpisodes`,
`FirstEpisodeAspects`) go 500 at a time and refuse more than 50,000.

Item keys (`Item.Key`): show -> `/Shows/{id}/Seasons`; season ->
`/Shows/{seriesId}/Episodes?seasonId={id}`; collection/folder ->
`/Items?parentId={id}`; leaves have none.

`SectionListing(key, type, view)` replaces the wall's Plex paths:

| view | listing |
|---|---|
| `az` | `/Items?parentId=L&recursive=true&includeItemTypes=Movie|Series&sortBy=SortName` |
| `added` | same, `sortBy=DateCreated,SortName&sortOrder=Descending` |
| `released` | same, `sortBy=PremiereDate,SortName&sortOrder=Descending` |
| `continue` | `/UserItems/Resume?parentId=L&mediaTypes=Video` |
| `folders` | `/Items?parentId=L` (not recursive), see below |
| `collections` | `/Items?includeItemTypes=BoxSet&recursive=true` (server-wide) |

### Artwork

An image ref is `itemId/Type/tag`, built only from tags the item reports
(`ImageTags.Primary/Logo/Thumb`, `BackdropImageTags[0]`; for episodes and
seasons the parent's `SeriesPrimaryImageTag`, `ParentBackdropImageTags`,
`ParentLogoImageTag`), so a ref never names a picture that does not exist.
Episodes keep the plex convention: `Thumb` is the show's poster, `Still` the
episode's own picture. `ImagePath`/`ImageURL` turn a ref into
`/Items/{id}/Images/{Type}?tag=...&fillWidth=&fillHeight=&quality=90&format=Jpg`
(cover-crop, as Plex's `minSize=1&upscale=1`) or `maxWidth=&maxHeight=&format=Png`
for logos. The tag changes when the picture does, so the disk cache key
(host + ref + size) never goes stale.

### Playback hand-off

`PlaybackInfo` passes the device profile through as raw JSON (the playback
design owns it). The answer's `MediaSources[].TranscodingUrl` is a server path
**with the session token embedded as `ApiKey=`**; `StreamURL` strips it so
the launcher sends `AuthHeader()` instead. `DirectStreamURL` builds
`/Videos/{id}/stream.{container}?static=true&mediaSourceId=...` for sources
that `SupportsDirectStream`. Audio/subtitle choices are stream indexes
(`Stream.ID`), sent per play in `PlaybackInfo` and in the reports.

## 4. Where Jellyfin 12.2 differed from expectations

* The `/Users/{userId}/Views|Items|Items/Resume|Items/Latest|PlayedItems`
  routes are gone from the OpenAPI document (still served). Use `/UserViews`,
  `/Items`, `/UserItems/Resume`, `/Items/Latest`, `/UserPlayedItems`.
* `/Items/Latest` returns a bare JSON array, not a `QueryResult`. For a TV
  library it mixes `Episode` and `Season` entries (grouped).
* `POST /QuickConnect/Initiate` with no body at all is answered `400` (through
  nginx over HTTP/2); with an empty body it works. The client always sends a
  body on POST.
* `POST /Users/AuthenticateWithQuickConnect` with an unapproved secret answers
  `404 "Error processing request."`, not 401.
* A 401 has an empty body.
* `PlaybackInfo`'s `TranscodingUrl` embeds the token as `ApiKey=`.
* `SortName` is generated, lowercased, with numbers zero-padded to ten digits
  (`"0000000003 body problem"`); the package strips the padding (the UI only
  reads its first letter).
* A movie library's top level, listed without `recursive`, is the films
  themselves: Jellyfin does not expose the on-disk folders of a `movies`
  library, so the Folders view is a flat listing there.
* Box sets are not children of a library; `collections` lists the server's.
* There is no first-character index endpoint; `FirstChars` makes 27 counting
  requests (`limit=0`, `nameLessThan=A`, `nameStartsWith=A..Z`).
* Stream languages are ISO 639-2 (`ara`, `zho`); `LangName` maps them.
* The test server had nothing in progress, so `resume.json` is assembled from
  captured episode/movie records with a resume point set.

## 5. Left for the UI swap (not done here)

* Replace `*plex.Client` with `*jellyfin.Client` in `ui` and `cmd/plexcrt`,
  add `ctx` arguments, and apply the field renames above.
* `wall.go`: rebuild `wallViews` on `SectionListing`; drop `FolderListing`
  and the `/library/collections/.../children` fallback (use `Item.Key`).
  `FirstChars` now takes the section type instead of a Plex query.
* Stream pickers: Jellyfin has no per-file default like Plex's
  `PUT /library/parts`. `SelectAudio`/`SelectSubtitle` are gone; the UI keeps
  the choice (per item or per show) and passes it to `PlaybackInfo` and the
  reports. The server's `RememberAudioSelections` user setting covers part
  of this server-side.
* `RemoveFromContinueWatching` clears the resume point (Jellyfin has no hide
  that keeps it), and cannot remove a Next Up entry. The UI text may need to say so.
* Sign-in screens (`login.go`, `connect.go`, `home_users.go`, `config.go`):
  server address -> `NormalizeHost` + `PublicInfo`; user/password -> `SignIn`
  or Quick Connect; persist `Credentials` through a `TokenStore` backed by
  the config; on `ErrUnauthorized` anywhere, return to sign-in. Plex Home,
  PINs, server discovery and `Reach`/`LocalNameBlocked` have no counterpart.
* `Item.Library` and `Subtype` are not filled (Jellyfin does not report a
  box set's content type or an item's library without extra requests).
* The playback launcher: device profile, transcode URL choice and the report
  cadence (`ReportProgress` about every 10 s) belong to step 5.
