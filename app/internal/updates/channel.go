package updates

// Channel is the build's release channel, set at link time
// (-X plexcrt/internal/updates.Channel=public|beta); "" is a development build.
var Channel string

// Installer is the Scripts entry that installs or repairs this build's channel:
// the one-file installer a user downloads and copies into Scripts
// (release/build_distribution.py names them). Installing adds no installer of
// its own, so a message that sends the user to one names this file.
func Installer() string {
	if Channel == "public" {
		return "MisterZine-Jellyfin-Install-Public"
	}
	return "MisterZine-Jellyfin-Install-Beta"
}
