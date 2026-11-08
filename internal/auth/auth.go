package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "task-tracker-api"
const audience = "task-tracker-client"

var ErrToken = errors.New("invalid or expired token")

type Tokens struct {
	secret []byte
	ttl    time.Duration
}

func New(secret string, ttl time.Duration) (*Tokens, error) {
	if len(secret) < 32 || ttl <= 0 {
		return nil, errors.New("JWT secret must be at least 32 bytes and TTL must be positive")
	}
	return &Tokens{secret: []byte(secret), ttl: ttl}, nil
}

func (t *Tokens) Issue(userID int64) (string, error) {
	if userID <= 0 {
		return "", ErrToken
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{Issuer: issuer, Audience: jwt.ClaimStrings{audience}, Subject: strconv.FormatInt(userID, 10),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

func (t *Tokens) Parse(raw string) (int64, error) {
	var claims jwt.RegisteredClaims
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuer(issuer), jwt.WithAudience(audience), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		return 0, ErrToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrToken
	}
	return id, nil
}
