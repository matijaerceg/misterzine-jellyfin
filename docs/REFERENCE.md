# Reference

Detailed behavior and display settings. For everyday help, see the
[help page](../release/README.md).

## Using MisterZine Jellyfin

### Signing in

When the sign-in opens, the app spends a moment looking for Jellyfin servers on
your local network. Any that answer are listed by name, with their address,
under **Servers on your network**, the server you last signed in to selected.
Choose one, or choose **Enter address** to type an address. If none answer,
the address keyboard opens straight away. OK during the search, or typing on a
USB keyboard, goes to the address without waiting. Discovery needs the server's
auto discovery setting (on by default) and UDP port 7359 open on the server.

The address is typed on the on-screen keyboard or a USB keyboard. The app
checks that a Jellyfin server answers there before going on; a server chosen
from the list is checked the same way, at the address it gave.

- An address with `http://` or `https://` is used as typed, including any path
  after the name (a server behind a reverse proxy at
  `https://example.com/jellyfin`, say).
- An address without either uses HTTPS, except one that ends in `:8096`,
  Jellyfin's plain HTTP port, which uses HTTP.
- A bare name or IP address with no port, such as `jellyfin.example.com`, is
  tried as `https://jellyfin.example.com` first and then as
  `http://jellyfin.example.com:8096`. The first that answers as a Jellyfin
  server is kept. When neither does, the message names the first address
  tried.
- Any other port without a scheme, such as `192.0.2.10:8920`, is tried over
  HTTPS only. Type `http://` in front for a plain HTTP port.

The server's name then shows with the ways to sign in:

- The users the server shows on its own login screen, if any, by name.
  Choose yourself: a user with a password types it, and one without is
  signed in at once. Servers often hide their users; the list then starts with
  Quick Connect.
- **Quick Connect** is listed when the server has it turned on. The screen
  shows a code; in a Jellyfin app where you are already signed in, open
  **Settings > Quick Connect** and enter it. The app checks every two seconds
  and signs in once the code is approved. An expired code asks for OK to get a
  new one.
- **User name and password** asks for both on the keyboard. Below a list of
  users it is called **Other user**.
- **Change server** returns to the servers found, or to the address.

Back steps back one screen. From the list of servers (or the search for them),
or from the address when no server was found, it opens Options, which holds
**Exit to MiSTer menu** until you have signed in. The session and the server's
address are saved in the settings file on the SD card. The password is not
saved.

Signing in to a different server forgets the hidden libraries and the audio and
subtitle choices kept for the old one. Signing in as a different user forgets
them too, and clears the recent searches. If the server refuses the saved
session (it was revoked, or the user removed), the app returns to sign-in with
the address kept and says so.

**Options > Switch user** lists the users of the server you are signed in to,
with the other ways to sign in, without typing its address again. Back returns
to Options, still signed in as before. Signing in replaces the saved sign-in,
so the app starts as the new user next time, and ends the previous session on
the server. If the server does not answer, OK tries again.

**Options > Sign out** ends the session on the server and forgets it, the
recent searches, the hidden libraries and the track choices. The server's
address stays for the next sign-in.

### Controls

- D-pad moves the selection; OK opens it; Back goes back. On Home, Back or Left
  at the start of a row opens the menu. L/R move a screen of posters along a
  Home row.
- Home shows **Continue Watching** (items in progress, followed by the next
  episodes Jellyfin suggests) and a **Recently Added** row for each library,
  ending in a **See all** tile that opens the library newest first. A library
  hidden in Options has no row.
- The menu lists Home, Search, the movie and TV libraries, Options and Exit to
  MiSTer menu. A long list of libraries scrolls, and the menu opens on the
  library visited last. Other library types (music, photos, mixed) are not
  listed.
- A library opens as a wall of posters, four across (six with the 16:9
  Display Shape), with tabs above: **A to Z**, **Added**, **Released**,
  **Continue** (the library's items in progress), **Folders** and, once the
  server has listed at least one, **Collections**. Up from the first row, or
  Back from anywhere in the grid, selects the tabs; Left and Right switch them;
  Back again leaves the library. L/R jump a letter in A to Z and a screen in
  the other views. The scrollbar on the right shows the letter, or the
  position and the total.
- **Folders** lists the library's folders as the server stores them. OK on a
  folder shows its subfolders and files under the folder's name; a file opens
  as from any other view; Back comes out one folder at a time. In a movie
  library Jellyfin lists the films without their folders, so the tab is a plain
  list there.
- **Collections** lists the server's collections. Jellyfin's collections do not
  belong to one library, so every movie and TV library shows the same ones.
  OK on a collection shows its items; Back returns to the tab. With the 4:3
  filter on, a collection holding nothing the filter keeps is left out.
- **Search** has an on-screen keyboard, and a USB keyboard types into it. It
  lists up to 30 movies and shows that match, through the 4:3 filter
  and without hidden libraries. Right from the keyboard's last column, or
  **Results**, moves to the list; Left returns. With nothing typed the list
  holds the last six searches. Back clears the query, then leaves.
- A movie's page holds **Play** (or **Resume from** and **Play from the
  start**), **Mark watched** or **Mark unwatched**, **Audio** when there is
  more than one track, and **Subtitles** when there are any. When the summary
  is cut short, Up from the first action selects it and OK shows it in full.
- A show with one season opens straight on its episodes. Otherwise its page
  lists the seasons; Down or OK opens one. An episode's actions are those of a
  movie page, then **Go to show** when the page was opened without the row of
  seasons (Home, search, a show with one season), and **Shuffle show**.
- A movie or episode you stopped partway through has **Forget my progress**
  as its last action. It clears the resume point on the server, for every
  Jellyfin app, so the item plays from the start and leaves Continue
  Watching. Jellyfin has no way to hide an item and keep its place. A next
  episode Jellyfin suggests has no progress to forget and does not offer it.
- A show's row of seasons ends with a **Shuffle** tile; **Shuffle show** does
  the same. Both play every episode of the show, watched or not and specials
  included, in a random order and each from its start. While the episodes are
  listed the start bar runs, and Back gives up. A shuffle runs on into the next
  episode even with autoplay off; Prev and Next move along the shuffled order.
- Back on a season page after playback selects the episode after the last one
  finished (played to the end or stopped in its last 30 seconds). An episode
  stopped earlier stays selected. Nothing starts playing.
- After three minutes without a press in the menus or on a paused video, the
  screen dims to a quarter of its brightness. The next press only brings it
  back; it does not also act. The sign-in screens never dim.
- To leave the app, choose Exit to MiSTer menu in the menu (before sign-in, at
  the foot of Options). Loading another core from MiSTer's OSD works too.

### Playback controls

- During playback, OK opens the controls: **-10**, **Pause**, **+10**,
  **Prev**, **Next**, **Audio**, **Subs** and **More**. Left and Right select
  a button and skip the ones with nothing to do (Prev on the first episode,
  Audio with one track); OK presses it. The controls hide after four seconds
  unless paused. Down closes them.
- On a USB keyboard, Space or P pauses and resumes playback in one press; a
  pause brings up the controls showing **Play**. A USB remote whose play key
  MiSTer does not pass through can be remapped to Space in MiSTer's keyboard
  mapping.
- Back steps out one level at a time. The first Back clears what is on screen:
  a list, the scrubber, the seek strip, a skip button or the controls. With
  nothing on screen, the next Back stops playback. A second Back within half
  a second of the one that cleared the screen is ignored, so a double press
  does not stop the video by surprise; what leaves the screen by itself, such
  as the controls hiding, leaves the next Back free to stop.
- Up opens the scrubber. A tap of Left or Right moves the dot ten seconds; a
  hold runs it, speeding up after half a second. The seek goes on its own
  400 ms after the last release; OK sends it at once. Down returns to the
  buttons and keeps the new position; Back returns without it.
- With the controls hidden, Left and Right show a strip with the title, the
  progress bar and the times, and work the same way: taps and holds add up into
  one seek. The last picture stays on screen until the new position plays.
- The progress bar shows how far is downloaded in pale purple. A seek inside
  that range can reuse what is already downloaded.
- **Audio** and **Subs** open a list of tracks. A change is kept for the movie
  or show. Another audio track, or a subtitle track the server burns in,
  restarts the stream at the same position; a subtitle track the app draws
  changes without a restart (see Subtitles).
- **More** holds **Crop**, for every picture, **Surround downmix
  boost** and **Subtitle size** (see Display setup, Sound and Subtitles),
  then:
  - **Autoplay next**, for an episode: On or Off with Left, Right or OK. It is
    the Options setting, saved at once, and decides this playback's end.
  - **Video bitrate**: Left and Right pick a step of the Options ladder, OK
    saves it. A transcode restarts at the same position with the new cap; a
    direct play (marked on the row) keeps playing and uses it next time.
  - **Playback info**: a read-only page, refreshed every second: direct play
    or transcode, the picture size and frame rate, the bitrate, the sound and
    its downmix, the subtitle track, the position, and the decoder's late
    frames and errors. OK or Back returns to More.
- Over an intro or credits Jellyfin has marked (its media segments),
  **Skip intro** or **Skip credits** shows at the lower right for 20 seconds once playback reaches
  them, also after a resume or a seek into them. OK skips to their end; Back
  dismisses the button. Options can turn the buttons off.
- When an episode plays to its end, the next one in the season is shown for
  ten seconds and then plays. OK plays it at once; Back stops. Options can
  turn this off.

### Options

Options lists, in groups:

- **Only show 4:3 media**: hides anything wider than 4:3 from the Home rows,
  search and every library view. Movies and episodes are judged by their own
  picture; a show by its first episode. TV libraries are read for that the
  first time the filter needs them, and the answers are kept in the cache
  folder. The filter relies on the aspect ratio Jellyfin records for each file,
  and Jellyfin sometimes records it wrongly or not at all, so some items may be
  missing from the filtered view or shown in it wrongly.
- **Autoplay next episode**: the countdown into the next episode.
- **Skip intro and credits buttons**.
- **Playback UI theme**: the look of the playback controls. **Standard**;
  **Icons**, Standard with the buttons drawn as small icons instead of
  words (back and forward 10 seconds as double arrows over a 10, pause bars
  or a play triangle, previous and next as arrows against a bar, a speaker
  for Audio, a caption box for Subs, three dots for More), in the same
  places, colours and order; or **VCR**, an old deck's on-screen display:
  bright green and white block capitals in an on-screen display chip's
  eight pure colours on a blue band, the progress as a row of blocks with an
  upright line for the scrubber's playhead, and the
  lines of the controls wavering sideways by a pixel, as a tape's picture
  did. **VCR, no band** leaves the band out, the text straight over the
  picture as on a deck. In both VCR looks the three dots that show while
  the picture is held up are a tape whose reels turn. The controls
  work the same in every look; the picture and the subtitles do not waver.
- **Video bitrate** (see Sound and playback quality).
- **Show** and a library's name, one row per library: off leaves it out of
  Home, the menu and search. Shown once the libraries have loaded.
- **Video calibration** and **Video crop** (see Display setup).
- Rows about your MiSTer INI, when it has a setting known to cause trouble:
  **HDMI refresh** (HDMI held at a fixed rate the core's output does not
  match), **HDMI sound** (DVI mode, no sound over HDMI) and **INI section** (a
  later `[MiSTer]` section overriding the app's `[MisterZine Jellyfin]`
  section). Select one for the file to change and what to add. See
  [HDMI setup](HDMI.md#settings-options-warn-about).
- **Surround downmix boost**, **Subtitle size** (see Subtitles), **Theme
  music** (a show's theme song on its pages, when Jellyfin has one) and
  **Navigation sounds** (no sound plays with the H.264 core).
- **Switch user** (see Signing in), once signed in, and **Sign out**, with the
  user's name.
- **Updates**, which says when an update is downloading, ready or available,
  or when one failed.
- **Send a report**.
- **Exit to MiSTer menu**, before sign-in only.
- **Version**. The foot of the screen shows the build.

Up and Down wrap between the first and last rows. OK, Left or Right turns a
setting on or off; a setting with more choices (playback UI theme, bitrate,
crop, downmix boost, subtitle size) changes with Left and Right only, shown by the arrows either
side of its value when it is selected. A scrollbar on the right shows where the
list is.

An update waiting under Options is marked with a purple UPDATE mark at the top
of Home and a purple dot in the menu.

### Connecting after power-on

Opened right after the MiSTer is switched on, the app can start before the
network is up. On a MiSTer without a clock chip it can also start before the
clock has been set from the internet, and secure (HTTPS) connections to the
server fail until then. For its first minute the app keeps trying every two
seconds and shows **Connecting** with what it is waiting for. After that it
shows the error and tries again every 30 seconds; OK tries at once. A server
name that does not exist is shown as an error at once.

### Release notes on start

A release can carry a short note. It appears once, over Home, the first time
Home has connected after the app starts, and stays until OK is held for one
second (a bar under the hint fills while it is held). Once dismissed, that
note is remembered in the settings file and does not show again.

### Subtitles and audio tracks

Jellyfin keeps no per-file choice of tracks, so the app keeps it: the audio and
subtitle tracks chosen on a page or during playback are saved for that movie,
or for the whole show, and passed with every play. Until you choose, the
server's defaults are used.

Text subtitle tracks (SRT, ASS/SSA, WebVTT) are drawn by the app over the
picture, as white type on a translucent box, centred along the bottom of the
screen. The video is sent without them, so a film with text subtitles can still
play untranscoded, and turning text subtitles on or off, choosing another text
track or changing their size does not restart the video. Only the words are
shown: styling, positioning and effects in ASS/SSA tracks are left out, and
accented letters lose their accents. Long lines wrap; up to six lines show at a
time.
Subtitles step aside while the controls, a list, the seek strip or a skip
button are on screen.

The first time a track embedded in the file is asked for, the server extracts
it, which can take a while in a large file. The video plays meanwhile, without
subtitles until they arrive. The app waits 30 seconds for an answer and tries
twice more, then gives up and shows none; turning subtitles off and on again
asks once more.

**Options > Subtitle size** chooses Normal, Large (150%) or Extra large (200%)
and is saved for future playback. During playback, **More > Subtitle size**
changes the same setting at once.

Picture subtitles (PGS, VobSub, DVB) are burned into the picture by Jellyfin.
The server must transcode the video for that, so it needs permission to
transcode for your account. Choosing one, or changing from one, restarts the
stream at the same position; the server can take a while to start the new
transcode. A text track in a script the app's fonts lack (Cyrillic, Greek,
Arabic, Chinese, Japanese, Korean and others) is handed to the server the same
way: the stream restarts once the app has fetched it, and the server burns it in
from then on until the app exits. Jellyfin cannot resize subtitles it burns in,
so Subtitle size does not change them, and a crop can cut their ends.
Subtitles the app draws are never cut by a crop.

### Sound and playback quality

Files the decoder can certainly play are streamed as stored: H.264 High or
Main, 8-bit, level 4.1 or lower, at most 1280x720, at most 30 frames per
second above 854 pixels wide (60 at or below), and at most 6 Mbit/s above 854
pixels wide (8 Mbit/s at or below), and no more than the **Video bitrate**
setting. Everything else, and anything with subtitles the server burns in (see
Subtitles), is transcoded by the server to H.264 within the same limits. If a
direct stream turns out to be unplayable or falls behind, playback switches
once to a transcode at the same position. See [playback details](PLAYBACK.md).

**Video bitrate** caps the video Jellyfin sends: 0.4, 1, 2, 3, 4, 6 or 8 Mbps
(the default and the most the decoder handles). The steps below 3 Mbps are for
a slow or unreliable link to the server, such as weak Wi-Fi. At those steps a
transcode is also made smaller, so the bits go further: 854x480 at 1 and
2 Mbps, 640x360 at 0.4 Mbps. That costs little on a 480i or 576i screen. A file
whose video is above the chosen bitrate is never streamed as stored; it is
transcoded to that bitrate. Start low on Wi-Fi and raise it while playback
stays smooth. The first run of a release with the decoder core sets it to the top value;
change it afterwards and it stays.

Sound is decoded on the MiSTer and folded to stereo. A plain fold of a
surround track comes out noticeably quieter than a stereo track, so
**Surround downmix boost** raises the level of surround sources: Off, Small or
Large (the default), with loud peaks softened rather than clipped. Stereo
tracks are passed through unchanged. **More > Downmix boost** in the
playback controls changes it without restarting the video.

### Update, rollback and remove

Open **Options > Updates** to see whether a newer version is available, and its
download size. The screen checks when it opens. Checks also run in the
background at startup and every six hours while browsing; failed background
checks stay quiet and are tried again after five minutes. An update found by an
earlier check is still offered while the network is down.

Updates offers only versions newer than the one running. It never reinstalls
the current version or goes back to an older one; **MisterZine-Jellyfin-Rollback**
does that. Stable builds notify about newer stable releases. Beta
notifications are off by default but can be switched on in Updates; a newer
beta is listed either way. Beta builds notify about newer betas and about
stable releases that catch up. Nothing installs or switches channels
automatically.

Choose **Update now**. Downloading continues if you leave the screen;
installing waits for **Restart now**. **Later** leaves the current version
active. If an update fails, the reason is shown under **Try again**, and the
current version keeps working. If the new version does not start, the previous
one is put back. Settings, sign-in, calibration and saved beta access are kept
through updates and rollback.

Downloads go through MiSTer Downloader into `misterzine-jellyfin-downloads` on
the SD card, and fall back to fetching the release file directly when that
fails. An ordinary Downloader or Update All run can refresh that download; it
cannot install it. Rebooting the MiSTer does not install a downloaded update
either; only **Restart now** does. The selected channel is registered with
Downloader in `downloader_misterzine_jellyfin.ini`. The app's own files live in
`/media/fat/misterzine-jellyfin`.

Each installed release keeps its own folder. Once an update has started, the
app keeps the new release and the previous one and removes older release
folders.

These entries are in the MiSTer **Scripts** menu once the app is installed.
Return to MiSTer Menu before running them.

- **MisterZine-Jellyfin-Rollback** selects the previous installed release
  without opening the app.
- **MisterZine-Jellyfin-Uninstall** works offline. Its default removes the
  app's programs, its menu, Scripts and Zaparoo entries, its boot entry,
  downloads and Downloader registration, and keeps sign-in, settings, artwork
  and saved beta access. Removing all of the app's data requires typing
  **REMOVE**. Downloader and unrelated files are kept. The same uninstaller is
  attached to each release on GitHub.
- **MisterZine-Jellyfin-Diagnostics** writes the same report as **Send a
  report** (below), for when the app does not start.

The installer adds **MisterZine Jellyfin** to the MiSTer main menu and a line
to `linux/user-startup.sh` that starts the small helper behind that entry at
boot. If Zaparoo is installed, the app also appears under **Other** in
Zaparoo, through its own file `zaparoo/launchers/misterzine-jellyfin.toml`.
Nothing needs adding to Zaparoo's `config.toml`. Each release checks the main
menu, Scripts and Zaparoo entries at boot and after an update to it, and puts
right any that are out of date. The boot line is changed only by the installer
and the uninstaller.

### Send a report

**Options > Send a report** gathers what helps diagnose a problem into
`/media/fat/misterzine-jellyfin/report.txt`: the app and launcher logs, the
reasons for connection and update failures (including `updates/last-error.log`),
the video settings read from the MiSTer INI, which MiSTer main is running and
the framebuffer state. It can name media titles and playback details. Sign-in
tokens, the server address and account files are left out, and beta access
codes are redacted. Online upload is not available for this app: the screen
says so and names the saved file, which you can attach to a problem report.
**MisterZine-Jellyfin-Diagnostics** writes the same file.

### Showcase captures

Select **Version** at the foot of Options and press OK three times in quick
succession (less than two seconds between presses) to toggle Showcase Mode. A
brief message confirms whether it is on or off. Showcase Mode uses generic
library names and hides library totals and position counts. Titles, artwork,
episode details and watched and progress marks remain visible. The user name is
also left off Sign out. The mode lasts until the app exits; repeat the shortcut
to turn it off sooner. After switching, Back returns to Home so cached menu
images cannot show old labels. This is capture styling, not full anonymization:
the sign-in screens can still show identifying information.

## Display setup

The output is chosen in the core's OSD (the MiSTer menu button while the app
runs), not in the app's Options. The core's own manual describes every option:
[core settings](../third_party/MiSTer_h264_decoder/site/content/using/settings.md).
In short:

- **Output Mode**: **480i60** (the default) and **576i50** are interlaced
  15 kHz signals for standard-definition CRTs; **480p60** and **576p50** are
  progressive, for 31 kHz displays, scalers and HDMI; **Native HDMI** plays
  video at its own size and leaves scaling to MiSTer's HDMI scaler. Use the
  576-line modes only with a display that accepts PAL.
- **Refresh Rate Match**: **50/60** lets playback switch between the 50 Hz and
  60 Hz families to suit the video, and returns to the chosen Output Mode when
  playback ends. Enable it only if your display accepts both.
- **240p Playback**: on the interlaced modes, shows video as 240p (288p in the
  PAL family) while menus stay interlaced.
- **Play 24fps at 25fps**: on the fixed 576-line modes with Refresh Rate Match
  Off, plays 23.976 and 24 fps films at 25 fps, the traditional PAL speed-up.
  Films run about 4% faster and the sound is pitched up.
- **Display Shape**: 4:3 or 16:9, shared by the menus, fitted video and the
  overlay. The app's screens widen for 16:9. In Native HDMI a playing video
  takes the HDMI mode's own shape.
- **De-flicker**: on interlaced output, softens thin horizontal lines in
  video and in the app's screens so they flicker less.

Restart playback after changing a setting that affects playback speed. There is
no 48 Hz mode.

**Options > Video calibration** shows adjustable screen edges and a square
with a circle in it. OK selects the next edge or the square's corner, the
D-pad moves it, and Back saves and leaves. The saved edges and width
correction also move video playback on a core that supports it (interface 1.2
or later, which the core shipped with the app does); menus are unaffected. On
an older H.264 core the screen is labelled **Video calibration (preview
only)**, and the saved values do not affect playback. Geometry applies to
fitted 480- and 576-line output, not to Native HDMI.

**Options > Video crop** sets how much of a picture that does not match the
screen is cut away. **Off** shows all of it, with black bars. **14:9** cuts the
sides of anything wider than 14:9, the compromise broadcasters used for
widescreen on 4:3 sets: thin bars remain and little is lost. **Fill** cuts
whatever overhangs the screen, so there are no bars: a 16:9 picture on a 4:3
screen loses a quarter of its width. A crop can cut the ends of long subtitle
lines burned in by the server, but not subtitles the app draws. Each playback
starts with this setting; **More > Crop** in the playback controls changes it
for that playback, including the episodes that follow it, and the change shows
at once.

For HDMI scaling, MiSTer INI sections and refresh rates, see
[HDMI setup](HDMI.md).

## Beta builds and supporter access

Official beta builds ask for the shared MisterZine supporter code when you
start or resume playback. Signing in, browsing, search and Options work
without it. The code is saved on the SD card and survives updates and
rollback. Stable releases and builds from this source need no code. See
[beta access](../release/BETA_ACCESS.md).

The code entry and the other parts that only official builds carry come from a
separate private repository; see [public source and supporter
builds](SUPPORTER_BUILDS.md). They currently add no other features.
