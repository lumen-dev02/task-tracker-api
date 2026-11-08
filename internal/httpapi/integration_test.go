package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lumen-dev02/task-tracker-api/internal/auth"
	"github.com/lumen-dev02/task-tracker-api/internal/model"
	"github.com/lumen-dev02/task-tracker-api/internal/service"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
	"github.com/lumen-dev02/task-tracker-api/internal/testutil"
	"github.com/lumen-dev02/task-tracker-api/migrations"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c client) call(method, path, body, token string, want int) []byte {
	c.t.Helper()
	r, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(r)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status %d, want %d, body %s", method, path, resp.StatusCode, want, data)
	}
	return data
}

func parse[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAPIWithPostgres(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s, err := service.New(store.New(pool))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.New(strings.Repeat("integration-secret", 3), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(s, tokens, pool.Ping, testLogger()))
	t.Cleanup(server.Close)
	c := client{t: t, base: server.URL, http: &http.Client{Timeout: 5 * time.Second}}
	c.call("GET", "/readyz", "", "", 200)
	const aliceBody = `{"email":"Alice@Example.com","name":"Alice","password":"alice-password-123"}`
	const bobBody = `{"email":"bob@example.com","name":"Bob","password":"bob-password-123"}`
	data := c.call("POST", "/api/v1/auth/register", aliceBody, "", 201)
	alice := parse[model.User](t, data)
	if strings.Contains(string(data), "password") || strings.Contains(string(data), "$2") || alice.Email != "alice@example.com" {
		t.Fatalf("registration leaks or normalization failed: %s", data)
	}
	bob := parse[model.User](t, c.call("POST", "/api/v1/auth/register", bobBody, "", 201))
	c.call("POST", "/api/v1/auth/register", aliceBody, "", 409)
	c.call("POST", "/api/v1/auth/login", `{"email":"alice@example.com","password":"wrong-password"}`, "", 401)
	c.call("POST", "/api/v1/auth/login", `{"email":"nobody@example.com","password":"wrong-password"}`, "", 401)
	login := func(email, password string) string {
		body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
		response := parse[map[string]string](t, c.call("POST", "/api/v1/auth/login", body, "", 200))
		if response["token_type"] != "Bearer" || response["access_token"] == "" {
			t.Fatal("missing bearer token")
		}
		return response["access_token"]
	}
	at := login("ALICE@example.com", "alice-password-123")
	bt := login("bob@example.com", "bob-password-123")
	if id, err := tokens.Parse(at); err != nil || id != alice.ID {
		t.Fatal("wrong login subject")
	}
	c.call("GET", "/api/v1/tasks", "", "", 401)
	created := parse[model.Task](t, c.call("POST", "/api/v1/tasks", `{"title":" Write tests ","description":"API permissions"}`, at, 201))
	if created.OwnerID != alice.ID || created.Title != "Write tests" || created.Status != "todo" || created.CreatedAt.IsZero() {
		t.Fatalf("created: %+v", created)
	}
	path := fmt.Sprintf("/api/v1/tasks/%d", created.ID)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		body := ""
		if method == "PUT" {
			body = `{"title":"Stolen","status":"done"}`
		}
		c.call(method, path, body, bt, 404)
	}
	unchanged := parse[model.Task](t, c.call("GET", path, "", at, 200))
	if unchanged.Title != created.Title {
		t.Fatal("foreign user changed task")
	}
	c.call("POST", "/api/v1/tasks", fmt.Sprintf(`{"title":"Spoof owner","owner_id":%d}`, bob.ID), at, 400)
	c.call("POST", "/api/v1/tasks", `{"title":"Bob task","status":"done"}`, bt, 201)
	second := parse[model.Task](t, c.call("POST", "/api/v1/tasks", `{"title":"Second","status":"done"}`, at, 201))
	list := parse[struct {
		Items  []model.Task `json:"items"`
		Limit  int          `json:"limit"`
		Offset int          `json:"offset"`
	}](t, c.call("GET", "/api/v1/tasks?limit=1&offset=0", "", at, 200))
	if len(list.Items) != 1 || list.Items[0].ID != second.ID || list.Limit != 1 || list.Offset != 0 {
		t.Fatalf("pagination/sort: %+v", list)
	}
	list = parse[struct {
		Items  []model.Task `json:"items"`
		Limit  int          `json:"limit"`
		Offset int          `json:"offset"`
	}](t, c.call("GET", "/api/v1/tasks?limit=1&offset=1", "", at, 200))
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("offset: %+v", list)
	}
	filtered := parse[struct {
		Items []model.Task `json:"items"`
	}](t, c.call("GET", "/api/v1/tasks?status=done", "", at, 200))
	if len(filtered.Items) != 1 || filtered.Items[0].ID != second.ID {
		t.Fatalf("filter or ownership: %+v", filtered)
	}
	for _, status := range []string{"in_progress", "done", "todo"} {
		updated := parse[model.Task](t, c.call("PUT", path, fmt.Sprintf(`{"title":"Updated","status":%q}`, status), at, 200))
		if updated.Status != status || updated.Description != "" || updated.CreatedAt != created.CreatedAt || updated.UpdatedAt.Before(created.UpdatedAt) {
			t.Fatalf("update: %+v", updated)
		}
	}
	c.call("PUT", path, `{"title":"No status"}`, at, 400)
	c.call("PUT", path, `{"title":"Bad status","status":"closed"}`, at, 400)
	c.call("GET", "/api/v1/tasks/9223372036854775807", "", at, 404)
	c.call("PUT", "/api/v1/tasks/9223372036854775807", `{"title":"Missing","status":"done"}`, at, 404)
	c.call("DELETE", path, "", at, 204)
	c.call("GET", path, "", at, 404)
	c.call("DELETE", path, "", at, 404)
	empty := string(c.call("GET", "/api/v1/tasks?status=in_progress", "", at, 200))
	if !strings.Contains(empty, `"items":[]`) {
		t.Fatalf("empty list must be array: %s", empty)
	}
	// PostgreSQL enforces invariants even when bypassing HTTP validation.
	if _, err := pool.Exec(ctx, `INSERT INTO tasks(owner_id,title,status) VALUES ($1,'invalid','closed')`, alice.ID); err == nil {
		t.Fatal("database accepted invalid status")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tasks(owner_id,title) VALUES (9223372036854775807,'orphan')`); err == nil {
		t.Fatal("database accepted missing owner")
	}

	t.Run("concurrent duplicate registration", func(t *testing.T) {
		var wg sync.WaitGroup
		codes := make(chan int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := c.http.Post(server.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"email":"race@example.com","name":"Race","password":"race-password-123"}`))
				if err != nil {
					t.Error(err)
					return
				}
				defer resp.Body.Close()
				codes <- resp.StatusCode
			}()
		}
		wg.Wait()
		close(codes)
		counts := map[int]int{}
		for code := range codes {
			counts[code]++
		}
		if counts[201] != 1 || counts[409] != 1 {
			t.Fatalf("concurrent registration: %v", counts)
		}
	})
}
