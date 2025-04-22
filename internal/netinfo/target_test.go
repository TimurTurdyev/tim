package netinfo

import "testing"

func TestParseTargetAS(t *testing.T) {
	for _, in := range []string{"as:13238", "AS13238", "as13238"} {
		got, err := ParseTarget(in)
		if err != nil || got.Kind != KindAS || got.Value != "as:13238" {
			t.Errorf("%s -> %+v %v", in, got, err)
		}
	}
	// Домены на "as" не должны превращаться в номер.
	if got, _ := ParseTarget("asana.com"); got.Kind != KindDomain {
		t.Errorf("asana.com -> %+v", got)
	}
}
