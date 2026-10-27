# Release hardware check

On each unit: install from Scripts and launch from the main menu; sign in with
a server address, once with Quick Connect and once with a user name and
password; browse; play, pause, seek and stop one item; leave a menu and a paused
item alone for three minutes and check the screen dims and the next press only
restores it; check theme music; leave to MiSTer Menu and relaunch. Check normal
startup has no test card.

Check each Output Mode the unit's display accepts, from the core OSD: 480i60 and
576i50 on a CRT, 480p60, 576p50 and Native HDMI on HDMI. With 240p Playback on,
check that menus stay interlaced and video switches. With Refresh Rate Match on,
play a 25 fps and a 24 or 30 fps item and check the output switches family and
returns when playback stops. On a 576-line mode with matching off, check Play
24fps at 25fps speeds up a 24 fps film.

For subtitles, use a file with an SRT track, an ASS/SSA track and a PGS or VobSub
track. With the SRT track, compare Normal, Large and Extra large on CRT and HDMI,
check the cues come and go with the speech, and check a long line wraps inside
the safe area with cropping on. Changing size, turning subtitles off and on, and
switching between text tracks should never reload the video, playing or paused.
Check Options and playback More show the same saved choice after relaunch, and
More is available for a 4:3 video too. The PGS or VobSub track should restart
the stream once and appear burned in; a Cyrillic or other non-Latin text track
should do the same after a moment. Note the first fetch time of an embedded
track in a large MKV.

Check the lowest bitrate steps on Wi-Fi.

Play an eligible file with Direct play on, then turn it off in More and back
on. Check Playback Info identifies each method, the position is retained, and
pause/resume and forward/backward seeks work in both modes. Compare startup,
seek recovery, sound buffer, late frames and decoder errors with the preceding
release using the same file, bitrate and network. Allow a warm-up before
comparing steady playback.

During a middle episode, tap L and R and check the episode does not change.
Hold each for two seconds and check the progress indicator leads to the previous
or next episode. Release early to cancel. At a season boundary, the unavailable
direction should do nothing.

Check HTTPS sign-in and playback with a valid certificate. A wrong server name,
untrusted certificate, changed server identity or unsafe redirect must fail
without sending credentials to the other destination. A local HTTP server must
show the unencrypted-connection warning before sign-in and remain playable after
approval; public HTTP must be refused. Use disposable fixture credentials for
these failure checks. Include direct streams and nested HLS playlist requests.

Check sign-in right after power-on, while the network and clock are still
settling, and that a revoked session returns to sign-in with the address kept.

Record the hardware and output combinations tested in the release notes.
Keep raw device logs and internal test journals outside this public repository.
