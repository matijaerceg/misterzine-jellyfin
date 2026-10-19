# Install the core

1. Download `MiSTer_h264_decoder_<version>.zip` from the latest release and extract it to
   the root of your SD card (`/media/fat`). It places `_Other/H264_YYYYMMDD.rbf` on the
   card, and the programs that come with it, each a script and a folder:
   - the Moonlight client: `Scripts/moonlight.sh` and `moonlight/`
     ([Streaming from a PC](../using/moonlight.md));
   - the Jellyfin client: `Scripts/jellyfin.sh` and `jellyfin/`
     ([Watching from a Jellyfin server](../using/jellyfin.md));
   - the video player: `Scripts/h264player.sh` and `h264player/`
     ([Playing video files](../using/player.md));
   - the camera viewer: `Scripts/h264cam.sh` and `h264cam/`
     ([Showing a USB camera](../using/camera.md)).
2. Run one from **Scripts** in the MiSTer menu, or install another program that uses the core
   (see [What you need](what-you-need.md)). The program loads the core itself when it starts
   playing.

## Version check

The OSD's About line shows the running build as `H264 <version> <yymmdd>`. Quote that line
when reporting a problem.
