package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-only-012345678901234567890123456789"

func TestRoundTrip(t *testing.T) {
	tokens, err := New(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tokens.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := tokens.Parse(raw)
	if err != nil || id != 42 {
		t.Fatalf("Parse = %d, %v", id, err)
	}
	if _, err := tokens.Issue(0); err == nil {
		t.Fatal("issued token for invalid subject")
	}
	if _, err := New("short", time.Hour); err == nil {
		t.Fatal("accepted short secret")
	}
	if _, err := New(testSecret, 0); err == nil {
		t.Fatal("accepted nonpositive TTL")
	}
}

func TestRejectInvalidTokens(t *testing.T) {
	tokens, err := New(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	valid := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": issuer, "aud": audience, "sub": "1", "exp": time.Now().Add(time.Hour).Unix()}
	}
	for _, tc := range []struct {
		name   string
		mutate func(jwt.MapClaims)
		method jwt.SigningMethod
		key    string
	}{
		{name: "expired", mutate: func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "no expiration", mutate: func(c jwt.MapClaims) { delete(c, "exp") }},
		{name: "wrong issuer", mutate: func(c jwt.MapClaims) { c["iss"] = "other" }},
		{name: "no issuer", mutate: func(c jwt.MapClaims) { delete(c, "iss") }},
		{name: "wrong audience", mutate: func(c jwt.MapClaims) { c["aud"] = "other" }},
		{name: "no audience", mutate: func(c jwt.MapClaims) { delete(c, "aud") }},
		{name: "invalid subject", mutate: func(c jwt.MapClaims) { c["sub"] = "abc" }},
		{name: "zero subject", mutate: func(c jwt.MapClaims) { c["sub"] = "0" }},
		{name: "negative subject", mutate: func(c jwt.MapClaims) { c["sub"] = "-1" }},
		{name: "future issued at", mutate: func(c jwt.MapClaims) { c["iat"] = time.Now().Add(time.Hour).Unix() }},
		{name: "future not before", mutate: func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }},
		{name: "wrong algorithm", method: jwt.SigningMethodHS384},
		{name: "wrong signature", key: strings.Repeat("x", 40)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			if tc.mutate != nil {
				tc.mutate(c)
			}
			method := tc.method
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			key := tc.key
			if key == "" {
				key = testSecret
			}
			raw, err := jwt.NewWithClaims(method, c).SignedString([]byte(key))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tokens.Parse(raw); err == nil {
				t.Fatal("accepted invalid token")
			}
		})
	}
	for _, raw := range []string{"", "not.a.token", "e30.e30.", strings.Repeat("x", 100)} {
		if _, err := tokens.Parse(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
