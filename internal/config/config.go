package config

import (
	"errors"
	"os"
)

const Placeholder = "replace-with-output-of-openssl-rand-hex-32"

type Config struct {
	DatabaseURL string
	JWTSecret   string
	HTTPAddr    string
}

func Load(migrateOnly bool) (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), JWTSecret: os.Getenv("JWT_SECRET"), HTTPAddr: os.Getenv("HTTP_ADDR")}
	if c.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = "127.0.0.1:8081"
	}
	if !migrateOnly && (len(c.JWTSecret) < 32 || c.JWTSecret == Placeholder) {
		return Config{}, errors.New("JWT_SECRET must be at least 32 bytes and not the example placeholder")
	}
	return c, nil
}
