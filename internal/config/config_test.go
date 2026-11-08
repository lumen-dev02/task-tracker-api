package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local/local")
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("HTTP_ADDR", "")
	c, err := Load(false)
	if err != nil || c.HTTPAddr != "127.0.0.1:8081" {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	t.Setenv("HTTP_ADDR", ":9090")
	c, err = Load(false)
	if err != nil || c.HTTPAddr != ":9090" {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	for _, secret := range []string{"", "short", Placeholder} {
		t.Setenv("JWT_SECRET", secret)
		if _, err := Load(false); err == nil {
			t.Fatalf("accepted secret %q", secret)
		}
		if _, err := Load(true); err != nil {
			t.Fatalf("migration requires JWT secret: %v", err)
		}
	}
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(true); err == nil {
		t.Fatal("accepted missing database URL")
	}
}
