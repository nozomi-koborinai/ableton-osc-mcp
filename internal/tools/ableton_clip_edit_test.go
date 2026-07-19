package tools

import "testing"

func TestClipWarpModeNames(t *testing.T) {
	cases := map[int]string{0: "Beats", 3: "Re-Pitch", 5: "REX", 6: "Complex Pro"}
	for idx, want := range cases {
		if got := nameForIndex(clipWarpModes, idx); got != want {
			t.Errorf("warp mode %d = %q, want %q", idx, got, want)
		}
	}
	if idx, ok := indexForName(clipWarpModes, "complex pro"); !ok || idx != 6 {
		t.Errorf("'complex pro' = (%d,%v), want (6,true)", idx, ok)
	}
	if idx, ok := indexForName(clipWarpModes, "2"); !ok || idx != 2 {
		t.Errorf("numeric '2' = (%d,%v), want (2,true)", idx, ok)
	}
	if _, ok := indexForName(clipWarpModes, "Nonsense"); ok {
		t.Error("unknown warp mode should not resolve")
	}
}
