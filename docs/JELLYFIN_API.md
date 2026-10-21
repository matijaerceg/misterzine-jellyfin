# Jellyfin API layer (`app/internal/jellyfin`)

`app/internal/jellyfin` is the app's only server client: every request the
menus, the artwork cache and the player make to a Jellyfin server goes through
it. `app/internal/ui` and `cmd/plexcrt` import it; nothing else talks to the
server. It replaced the Plex client the app started from, and some field names
still follow that client (see [Item fields](#item-fields-and-the-plex-names-they-replace)).
The server behaviour below was captured against Jellyfin 12.2.0 on 2026-10-15.

## 1. How the app uses it

**One client per session.** `ui.NewClient` makes the client for the session on
record in the settings (`config.go`): the server address, the session's
credentials and the install's `ClientID` as the device id. The client gets its
own copy of the credentials (a `MemoryStore`), so request goroutines never read
the settings the UI thread edits. `App.Client` holds it, and `App.Connect`
hands the same client to the artwork loader (`Art`), the poster-wall pagers
and the player. `cmd/plexcrt/main.go` makes it at start when a session is on
record; otherwise the app opens on the sign-in screen.

**Signing in** (`login.go`). The typed address goes through `HostCandidates`
and each candidate is probed with `PublicInfo` (a bare name tries https, then
http on port 8096). Then Quick Connect (`QuickConnectEnabled`, `Start`,
`Check`, `Finish`) or a user name and password (`SignIn`). The resulting
`Credentials` are saved in the settings (`Config.SetCredentials`), which is
the `TokenStore` that outlives the client; `Connect` then makes a new client.
A token file passed with `-host` and `-token` is imported in `main.go`, with
`Me` to learn the user id.

**When the session ends.** Any load that fails with `ErrUnauthorized` (the
home rows, libraries, search, playback) goes to `App.SessionEnded`, which
forgets the session, keeps the server address and returns to the sign-in
screen with a notice. **Sign out** in Options forgets the session the same way
and revokes the token in the background (`SignOut`).

**Browsing.** Where the UI calls the client:

| Methods | Where |
|---|---|
| `Hubs`, `SectionRecent`, `Sections` | `home.go` (home rows), `app.go` (library list) |
| `Page` with `SectionListing` and the `View*` names | `pager.go`, `wall.go` (every poster wall and its tabs) |
| `Items` | `app.go`, `season_data.go`, `view_refresh.go` (a show's seasons and episodes, a collection) |
| `Item` | `preplay.go`, `season.go`, `playing.go`, `view_refresh.go`, `wall.go`, `home.go`, `app.go` (the full item before play, refreshes after) |
| `FirstChars` | `wall.go` (the alphabet jump) |
| `ShowEpisodes` | `app.go` (shuffle) |
| `ShowAspect`, `FirstEpisodeAspects`, `Page` | `aspects.go` (the picture shape of each title, swept in the background) |
| `ContinueWatching`, `RemoveFromContinueWatching` | `continue_watching.go` |
| `Scrobble` | `preplay.go`, `season.go` (mark watched or unwatched) |
| `Search` | `search.go` |
| `Art`, `Logo`, `ArtData`, `LogoData` | `art.go` (posters, backdrops and logos, cached on the SD card) |
| `ThemeFile` | `theme.go` (a show's theme song) |

**Playing.** `directplay.go` asks the server how to play an item with
`PlaybackInfo`: the device profile, the chosen audio and subtitle streams and
the start point (see [PLAYBACK.md](PLAYBACK.md) for the decision). `player.go`
turns the answer into a URL with `StreamURL` (a transcode) or
`DirectStreamURL` (the file as stored), hands it to the player with
`AuthHeader()` so the token stays out of the URL, and reports the session with
`ReportStart`, `ReportProgress` (every 10 s) and `ReportStopped`. Jellyfin has
no per-file default track, so the app keeps the audio and subtitle choice per
movie or show in its settings (`Config.Streams`) and sends it with every
`PlaybackInfo` and report.

## 2. The package's surface

Every method that does I/O takes a `context.Context` first.

```go
c := jellyfin.New(host, cacheDir, deviceID, store) // store: TokenStore (nil = memory)
c.SignIn(ctx, user, pw)            // -> Credentials{UserID, UserName, Token, ServerID}
c.QuickConnectEnabled / QuickConnectStart / QuickConnectCheck / QuickConnectFinish
c.PublicInfo(ctx)  c.Me(ctx)  c.SignOut(ctx)  c.SignedIn()  c.UserID()
jellyfin.NormalizeHost(typed)  jellyfin.HostCandidates(typed)

c.Sections(ctx) []Section
c.Page(ctx, listing, q, start, size) ([]*Item, total, error)
c.Items(ctx, listing, q, max)
jellyfin.SectionListing(sectionKey, sectionType, view) (listing, q) // view: ViewAZ, ViewAdded, ...
c.Item(ctx, id)            // full: media sources, streams, markers (c.Markers)
c.Seasons / ShowEpisodes / ShowAspect / FirstEpisodeAspects / FirstChars(ctx, section, sectionType)
c.Hubs(ctx, n)  c.SectionRecent(ctx, key, n)  c.ContinueWatching(ctx)  c.RemoveFromContinueWatching(ctx, id)
c.Scrobble(ctx, id, watched)  c.Search(ctx, q)
c.Art / Logo / ArtData / LogoData (ctx, ref, w, h)  c.ImageURL(ref, ImageQuery)  c.ThemeFile(ctx, showID, songID)
c.PlaybackInfo(ctx, id, PlaybackRequest)  c.StreamURL / DirectStreamURL
c.ReportStart / ReportProgress / ReportStopped(ctx, PlayState)
c.AuthHeader()  // for the player: same header on the stream request
jellyfin.ErrUnauthorized, *jellyfin.HTTPError, Fold, LangName, Ticks, Seconds
```

### Item fields and the Plex names they replace

The app began on a Plex client, and `Item` keeps that client's field names
where the meaning carried over. These are the ones that changed:

| Plex client | jellyfin | note |
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
collection types (music, mixed, homevideos, ...) are skipped. `Hub.Ident` is
`home.continue` or `home.recent.<library id>`. `Item.Library` and `Subtype`
are not filled: Jellyfin does not report a box set's content type or an
item's library without extra requests.

### Errors

* `401` -> an `*HTTPError` for which `errors.Is(err, ErrUnauthorized)` holds.
  Not retried. The UI returns to sign-in on it (`App.SessionEnded`); on the
  sign-in screen, `SignIn` with a wrong password is also `ErrUnauthorized`.
* Other non-2xx -> `*HTTPError{Method, Path, Status}`. The message holds only
  the path, never the query (Quick Connect secrets) or the token.
* GET and DELETE are retried on network errors and 5xx: `Retries` (2) more
  attempts, `Backoff` 250 ms then x3, aborting on context cancellation.
  POSTs (sign-in, reports, played state) are never retried.
* Timeouts: 10 s per request, 30 s for the home rows.
* `Logf` (optional) gets retry notices only: method and path, no query. The
  app does not set it.

## 3. Jellyfin mapping

### Authorization

Every request, including images and the stream fetch:

```
Authorization: MediaBrowser Client="MisterZine", Device="MiSTer CRT", DeviceId="<stable id>", Version="0.1", Token="<token>"
```

`Token="..."` is added once signed in. `DeviceId` must be stable per install
(Jellyfin keys sessions and Quick Connect on it); the app passes the
`ClientID` from its settings. The token is never put in a URL: Jellyfin 12
refuses `?api_key=` on the `/socket` websocket (REST still accepts it, but URLs
end up in logs and in `ps`). `AuthHeader()` is exported so the player sends
the same.

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
| Text subtitles | `GET /Videos/{id}/{mediaSourceId}/Subtitles/{index}/Stream.srt` (the whole track as SRT, item times) | |
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

`SectionListing(key, type, view)` is the listing of each tab of a library's
poster wall (`wall.go`):

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
For an episode, `Thumb` is the show's poster and `Still` the episode's own
picture. `ImagePath`/`ImageURL` turn a ref into
`/Items/{id}/Images/{Type}?tag=...&fillWidth=&fillHeight=&quality=90&format=Jpg`
(cover-crop) or `maxWidth=&maxHeight=&format=Png`
for logos. The tag changes when the picture does, so the disk cache key
(host + ref + size) never goes stale.

### Playback hand-off

`PlaybackInfo` passes the device profile through as raw JSON (the UI builds it;
see [PLAYBACK.md](PLAYBACK.md)). The answer's `MediaSources[].TranscodingUrl` is a server path
**with the session token embedded as `ApiKey=`**; `StreamURL` strips it so
the player sends `AuthHeader()` instead. `DirectStreamURL` builds
`/Videos/{id}/stream.{container}?static=true&mediaSourceId=...` for sources
that `SupportsDirectStream`. Audio/subtitle choices are stream indexes
(`Stream.ID`), sent per play in `PlaybackInfo` and in the reports. A subtitle
stream with `IsTextSubtitleStream` and `SupportsExternalStream` is a text track
(`Stream.Text`): `SubtitleText` fetches it, and `PlaybackInfo` is then sent
`SubtitleStreamIndex=-1` because the app draws it. A start position in the
subtitle path would drop the cues before it and shift the rest, so the app
never sends one.

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

## 5. What the Plex client had that this one does not

* Per-file default tracks (Plex's `PUT /library/parts`): there is no
  `SelectAudio`/`SelectSubtitle`. The app keeps the choice itself
  (`Config.Streams`) and passes it with every play. The server's
  `RememberAudioSelections` user setting covers part of this server-side.
* Hiding an item from Continue Watching while keeping its place:
  `RemoveFromContinueWatching` clears the resume point, and cannot remove a
  Next Up entry.
* Sign-in by PIN, server discovery, home users and connection probing
  (Plex's `Reach`/`LocalNameBlocked`) have no counterpart. Sign-in is the
  server address -> `HostCandidates` + `PublicInfo` (a bare name tries https,
  then http on port 8096); user/password -> `SignIn` or Quick Connect.
