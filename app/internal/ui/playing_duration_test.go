package ui

import "testing"

func TestPlayingDurationTranscodeTakesTheItems(t *testing.T) {
	for _, c := range []struct {
		item, player float64
		direct       bool
		want         float64
	}{
		// a transcode: the player's is the item's plus the start offset
		{1402, 1913, false, 1402},
		{1402, 0, false, 1402},
		{0, 1913, false, 1913},
		{0, 0, false, 0},
		{0, -1, false, 0},
		// a direct stream: the player's is exact when it has one
		{1402, 1398.5, true, 1398.5},
		{1402, 0, true, 1402},
		{1402, -1, true, 1402},
		{0, 1913, true, 1913},
		{0, 0, true, 0},
	} {
		if got := playingDuration(c.item, c.player, c.direct); got != c.want {
			t.Errorf("playingDuration(%v, %v, %v) = %v, want %v", c.item, c.player, c.direct, got, c.want)
		}
	}
}
