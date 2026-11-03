package session

import (
	"testing"
	"time"
)

func TestTheLatestOutputOfTheWantedTabsCounts(t *testing.T) {
	now := time.Unix(1000, 0)
	wanted := map[int]bool{1: true, 2: true}
	for name, test := range map[string]struct {
		listing string
		want    int64
	}{
		"latest of two":      {"1\t100\n2\t300\n", 300},
		"latest first":       {"2\t300\n1\t100\n", 300},
		"another tab's":      {"0\t900\n1\t100\n", 100},
		"none wanted":        {"0\t900\n", 0},
		"from the future":    {"1\t5000\n", 1000},
		"no value":           {"1\t\n", 0},
		"zero":               {"1\t0\n", 0},
		"not a number":       {"x\t100\n1\tabc\n", 0},
		"an empty listing":   {"", 0},
		"a line without tab": {"1 100\n", 0},
	} {
		got := latestOutputFromListing([]byte(test.listing), wanted, now)
		if test.want == 0 {
			if !got.IsZero() {
				t.Errorf("%s: %v", name, got)
			}
			continue
		}
		if got.Unix() != test.want {
			t.Errorf("%s: %v", name, got.Unix())
		}
	}
}
