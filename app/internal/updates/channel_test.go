package updates

import "testing"

// Messages that send the user to an installer name one that exists: the
// downloaded Install-Beta or Install-Public, never a bare Install script.
func TestInstallerNamesTheChannelsScript(t *testing.T) {
	was := Channel
	t.Cleanup(func() { Channel = was })
	for channel, want := range map[string]string{
		"public": "MisterZine-Jellyfin-Install-Public",
		"beta":   "MisterZine-Jellyfin-Install-Beta",
		"":       "MisterZine-Jellyfin-Install-Beta",
	} {
		Channel = channel
		if got := Installer(); got != want {
			t.Errorf("channel %q: %s, want %s", channel, got, want)
		}
	}
}
