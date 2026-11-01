// Package totp implements RFC 6238 time-based one-time passwords (SHA1, 6 digits, 30s),
// which is what the STACKIT IDP hands out as "Geheimschlüssel".
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

const (
	Period = 30 * time.Second
	Digits = 6
)

// Normalize strips whitespace/dashes and upper-cases a base32 secret.
func Normalize(secret string) string {
	s := strings.ToUpper(secret)
	s = strings.NewReplacer(" ", "", "-", "", "=", "").Replace(s)
	return s
}

// Code computes the TOTP for secret at time t.
func Code(secret string, t time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(Normalize(secret))
	if err != nil {
		return "", fmt.Errorf("decode base32 secret: %w", err)
	}
	counter := uint64(t.Unix()) / uint64(Period/time.Second)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, bin%mod), nil
}

// Now returns the current code and the seconds it stays valid.
func Now(secret string) (code string, validFor time.Duration, err error) {
	t := time.Now()
	code, err = Code(secret, t)
	if err != nil {
		return "", 0, err
	}
	elapsed := time.Duration(t.Unix()%int64(Period/time.Second)) * time.Second
	return code, Period - elapsed, nil
}
