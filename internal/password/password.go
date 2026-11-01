// Package password generates passwords equivalent to `pwgen -cny 16`:
// 16 characters, at least one capital letter (-c), one digit (-n) and one symbol (-y).
package password

import (
	"crypto/rand"
	"math/big"
)

const (
	lower   = "abcdefghijklmnopqrstuvwxyz"
	upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits  = "0123456789"
	symbols = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~" // pwgen's -y symbol set
	all     = lower + upper + digits + symbols
)

// Generate returns a random password of the given length satisfying the pwgen -cny rules.
func Generate(length int) string {
	if length < 4 {
		length = 4
	}
	for {
		b := make([]byte, length)
		for i := range b {
			b[i] = all[randInt(len(all))]
		}
		if hasAny(b, upper) && hasAny(b, digits) && hasAny(b, symbols) && hasAny(b, lower) {
			return string(b)
		}
	}
}

func hasAny(b []byte, set string) bool {
	for _, c := range b {
		for i := 0; i < len(set); i++ {
			if c == set[i] {
				return true
			}
		}
	}
	return false
}

func randInt(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return int(v.Int64())
}
