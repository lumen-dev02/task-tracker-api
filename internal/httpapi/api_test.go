package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lumen-dev02/task-tracker-api/internal/auth"
	"github.com/lumen-dev02/task-tracker-api/internal/service"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func unitAPI(t *testing.T) (http.Handler, string) {
	t.Helper()
	s, err := service.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.New(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tokens.Issue(1)
	if err != nil {
		t.Fatal(err)
	}
	return New(s, tokens, func(context.Context) error { return nil }, testLogger()), raw
}

func perform(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAuthenticationAndRouting(t *testing.T) {
	h, token := unitAPI(t)
	for _, tc := range []struct {
		method, path, token string
		status              int
	}{
		{"GET", "/healthz", "", 200},
		{"GET", "/readyz", "", 200},
		{"GET", "/api/v1/tasks", "", 401},
		{"GET", "/api/v1/tasks/1", "invalid", 401},
		{"POST", "/api/v1/tasks", "", 401},
		{"PUT", "/api/v1/tasks/1", "", 401},
		{"DELETE", "/api/v1/tasks/1", "", 401},
		{"GET", "/api/v1/tasks/0", token, 400},
		{"GET", "/api/v1/tasks/-1", token, 400},
		{"GET", "/api/v1/tasks/abc", token, 400},
		{"GET", "/api/v1/tasks/999999999999999999999", token, 400},
		{"PATCH", "/api/v1/tasks/1", token, 405},
		{"GET", "/unknown", "", 404},
	} {
		w := perform(h, tc.method, tc.path, "", tc.token)
		if w.Code != tc.status {
			t.Errorf("%s %s: %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if w.Code == 401 && w.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Error("missing auth challenge")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Error("missing cache control")
		}
	}
	for _, headers := range [][]string{{"Basic " + token}, {"Bearer"}, {"Bearer " + token + " extra"}, {"Bearer " + strings.Repeat("x", 4097)}, {"Bearer " + token, "Bearer " + token}} {
		r := httptest.NewRequest("GET", "/api/v1/tasks", nil)
		for _, header := range headers {
			r.Header.Add("Authorization", header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("invalid authorization: %d", w.Code)
		}
	}
}

func TestJSONAndQueryValidation(t *testing.T) {
	h, token := unitAPI(t)
	for _, body := range []string{"", "{", "[]", "null", `{}`, `{"title":" "}`, `{"title":"x","status":"bad"}`, `{"title":42}`, `{"title":"x","owner_id":2}`, `{"title":"x"} {}`, `{"title":"x"} null`} {
		w := perform(h, "POST", "/api/v1/tasks", body, token)
		if w.Code != 400 {
			t.Errorf("body %q: %d", body, w.Code)
		}
	}
	w := perform(h, "POST", "/api/v1/tasks", `{"title":"`+strings.Repeat("x", 66000)+`"}`, token)
	if w.Code != 413 {
		t.Fatalf("oversize body: %d", w.Code)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=abc", "limit=", "offset=-1", "offset=100001", "offset=abc", "status=unknown", "owner_id=2", "limit=1&limit=2", "limit=%ZZ", "limit=1;offset=0"} {
		w := perform(h, "GET", "/api/v1/tasks?"+query, "", token)
		if w.Code != 400 {
			t.Errorf("query %q: %d", query, w.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	a := &API{log: testLogger()}
	for _, tc := range []struct {
		err    error
		status int
	}{
		{service.ErrValidation, 400}, {service.ErrCredentials, 401}, {store.ErrEmailTaken, 409}, {store.ErrNotFound, 404}, {context.DeadlineExceeded, 503}, {context.Canceled, 503}, {errors.New("password=do-not-leak"), 500},
	} {
		w := httptest.NewRecorder()
		a.fail(w, tc.err)
		if w.Code != tc.status {
			t.Errorf("%v: %d", tc.err, w.Code)
		}
		if strings.Contains(w.Body.String(), "do-not-leak") {
			t.Fatal("internal details leaked")
		}
	}
	s, err := service.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.New(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := New(s, tokens, func(context.Context) error { return errors.New("db unavailable") }, testLogger())
	if w := perform(h, "GET", "/readyz", "", ""); w.Code != 503 {
		t.Fatal(w.Code)
	}
}
