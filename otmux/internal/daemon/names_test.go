package daemon

import (
	"strings"
	"testing"
)

func TestTabNamesEndInIte(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range tabNames {
		if !strings.HasSuffix(n, "ite") || n != strings.ToLower(n) || seen[n] {
			t.Errorf("tab name %q: want a unique lowercase word ending in -ite", n)
		}
		seen[n] = true
	}
}
