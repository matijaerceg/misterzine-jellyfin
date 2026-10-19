package updates

// Channel is the build's release channel, set at link time
// (-X plexcrt/internal/updates.Channel=public|beta); "" is a development build.
var Channel string
