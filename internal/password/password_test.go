package password

import (
	"strings"
	"testing"
)

func TestGenerateMeetsPolicy(t *testing.T) {
	for i := 0; i < 200; i++ {
		p := Generate(16)
		if len(p) != 16 {
			t.Fatalf("len %d: %q", len(p), p)
		}
		if !strings.ContainsAny(p, upper) || !strings.ContainsAny(p, lower) || !strings.ContainsAny(p, digits) || !strings.ContainsAny(p, symbols) {
			t.Fatalf("policy violated: %q", p)
		}
	}
}
