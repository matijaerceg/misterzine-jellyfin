//go:build !premium

package ui

import "testing"

func TestSourceBuildNeedsNoSupporterAccess(t *testing.T) {
	a := betaTestApp(t)
	if err := a.checkPlaybackAccess(); err != nil {
		t.Fatal(err)
	}
	if !a.requirePlaybackAccess(func() { t.Fatal("unexpected deferred action") }) {
		t.Fatal("source build locked")
	}
	for _, o := range NewOptions(a).items() {
		if o.label == "Beta access" {
			t.Fatal("source build offers code entry")
		}
	}
	if PremiumVersion() != "" {
		t.Fatal("source build advertises supporter overlay")
	}
}
