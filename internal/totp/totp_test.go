package totp

import (
	"testing"
	"time"
)

// RFC 6238 test vector (SHA1, secret "12345678901234567890" = base32 GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ)
func TestRFC6238(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for ts, want := range cases {
		got, err := Code(secret, time.Unix(ts, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("t=%d: got %s want %s", ts, got, want)
		}
	}
}

func TestStackitStyleSecret(t *testing.T) {
	if _, err := Code("GFAWM4CGNRAVAZJSPA4TQWTZJA", time.Now()); err != nil {
		t.Fatal(err)
	}
}
