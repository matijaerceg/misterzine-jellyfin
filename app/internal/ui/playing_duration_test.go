package ui

import "testing"

func TestPlayingDurationPrefersTheItems(t *testing.T) {
	for _, c := range []struct{ item, player, want float64 }{
		{1402, 1913, 1402},
		{1402, 0, 1402},
		{0, 1913, 1913},
		{0, 0, 0},
		{0, -1, 0},
	} {
		if got := playingDuration(c.item, c.player); got != c.want {
			t.Errorf("playingDuration(%v, %v) = %v, want %v", c.item, c.player, got, c.want)
		}
	}
}
