# MisterZine Jellyfin 0.1.0-beta.3

Third beta: scrubbing shows frames of the film, the playback controls come in
four looks, the menus scroll faster, and supporters get screensavers, a choice
of Home logo and a Random tab.

- Holding Left or Right on the scrubber shows a frame of the film at the dot,
  when the library has trickplay images turned on in Jellyfin. Playback pauses
  while the hold runs and resumes at the seek. A dark box with the wait dots
  stands in for a frame that is still loading.
- Text subtitles are drawn as white type with a thin dark outline.
  **Subtitle background** in Options puts them on a translucent box instead.
- **Playback UI theme** chooses the look of the playback controls: Standard,
  Icons, VCR or VCR without its band. The looks other than Standard are a
  supporter extra.
- **More** in the playback controls is one list: Crop, Downmix boost,
  Subtitle size, Autoplay next and Video bitrate change in place with Left and
  Right, and Playback info shows direct play or transcode, the picture, the
  sound and the decoder's late frames.
- Supporter extras, each with the purple star in Options: **Screensavers**
  (a bouncing logo or a slideshow of your libraries' art), **Home logo**
  (the wordmark, MisterKun or none) and the **Random** tab in libraries. OK
  on a locked row opens the code entry; a code keeps its month's extras on the
  card for good.
- A library wall rounds from its last row to its first, a held direction
  keeps up with the pictures, and letter jumps fetch nothing until they stop.
  **Forget my progress** on an item in Continue Watching clears its resume
  point on the server.
- Space or P on a USB keyboard pauses and resumes in one press. During
  playback the first Back clears what is on screen, and a second Back within
  half a second is ignored, so a double press no longer stops the video.
- The resume point reaches every screen the moment a playback ends, direct
  play uses the player's own length, and the MiSTer menu loads after Exit.
- Every install starts once at the top video bitrate, now that the decoder is
  in the FPGA.

Playback updates Jellyfin watched state and resume positions. Network
seeking, server compatibility and different hardware setups remain beta test
areas. The included FPGA image is the beta.2 design, unchanged.

Installers and update notices become available when this release and its
catalogue are published. See README.md for setup and feedback instructions.
