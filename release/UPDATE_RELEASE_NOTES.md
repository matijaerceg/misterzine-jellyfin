# MisterZine Jellyfin 0.1.0-beta.2

Second beta: fixes from beta.1 testing, subtitles drawn by the app, easier
sign-in and smoother menus.

- Text subtitles (SRT, ASS/SSA, WebVTT) are drawn by the app at Normal, Large
  or Extra large size. Choosing, hiding or resizing them never restarts the
  video, and a file with text subtitles can still play directly. Picture
  subtitles, and text in scripts the app's fonts lack, are still burned in by
  the server.
- Sign-in lists the Jellyfin servers found on the local network, then the
  users a server shows. **Options > Switch user** changes user without typing
  the address again. A bare server name that gives no answer is tried again as
  `http://name:8096`.
- Video bitrate offers 0.4, 1 and 2 Mbps for weak Wi-Fi, with a smaller
  transcoded picture. A file above the chosen bitrate is transcoded to it
  instead of streamed as stored.
- Chosen audio and subtitle tracks stay chosen on a movie page, and an
  episode's Audio and Subtitles actions no longer disappear after switching
  seasons.
- The 4:3 filter leaves out collections it would empty.
- Options shows the display check again (HDMI refresh, HDMI sound, INI
  section), judged against the core's Output Mode and Refresh Rate Match.
- Menus drop far fewer frames, held directions stop where they are released,
  Options changes save in the background, and leaving a show with theme music
  no longer pauses. If a settings save fails, the change stays for the session
  and a notice says so.
- Running the installer again over the installed release reports it as up to
  date, restores the Scripts entries, main-menu entry, boot hook and Downloader
  entry, and restarts the app.

Playback updates Jellyfin watched state and resume positions. Network seeking,
server compatibility and different hardware setups remain beta test areas.
The included FPGA image is the beta.1 design rebuilt to report
`jf-0.1.0-beta.2`; its original qualification manifest is included so the
image remains traceable.

Installers and update notices become available when this release and its
catalogue are published. See README.md for setup and feedback instructions.
