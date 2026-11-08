package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lumen-dev02/task-tracker-api/internal/auth"
	"github.com/lumen-dev02/task-tracker-api/internal/model"
	"github.com/lumen-dev02/task-tracker-api/internal/service"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
)

type API struct {
	service *service.Service
	tokens  *auth.Tokens
	ready   func(context.Context) error
	log     *slog.Logger
}

func New(s *service.Service, tokens *auth.Tokens, ready func(context.Context) error, logger *slog.Logger) http.Handler {
	a := &API{service: s, tokens: tokens, ready: ready, log: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.ready(r.Context()); err != nil {
			a.log.Error("readiness failed", "error", err)
			errorResponse(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /api/v1/auth/register", a.register)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/tasks", a.protect(a.createTask))
	mux.HandleFunc("GET /api/v1/tasks", a.protect(a.listTasks))
	mux.HandleFunc("GET /api/v1/tasks/{id}", a.protect(a.getTask))
	mux.HandleFunc("PUT /api/v1/tasks/{id}", a.protect(a.updateTask))
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", a.protect(a.deleteTask))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *API) protect(next func(http.ResponseWriter, *http.Request, int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			unauthorized(w)
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 4096 {
			unauthorized(w)
			return
		}
		id, err := a.tokens.Parse(parts[1])
		if err != nil {
			unauthorized(w)
			return
		}
		next(w, r, id)
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	errorResponse(w, http.StatusUnauthorized, "invalid or missing access token")
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := a.service.Register(r.Context(), in.Email, in.Name, in.Password)
	if err != nil {
		a.fail(w, err)
		return
	}
	jsonResponse(w, http.StatusCreated, u)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := a.service.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		a.fail(w, err)
		return
	}
	token, err := a.tokens.Issue(u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "Bearer"})
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request, owner int64) {
	var in model.TaskInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.service.CreateTask(r.Context(), owner, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/tasks/"+strconv.FormatInt(t.ID, 10))
	jsonResponse(w, http.StatusCreated, t)
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request, owner int64) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	t, err := a.service.GetTask(r.Context(), owner, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, t)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request, owner int64) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	var in model.TaskInput
	if !decode(w, r, &in) {
		return
	}
	t, err := a.service.UpdateTask(r.Context(), owner, id, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, t)
}

func (a *API) deleteTask(w http.ResponseWriter, r *http.Request, owner int64) {
	id, ok := taskID(w, r)
	if !ok {
		return
	}
	if err := a.service.DeleteTask(r.Context(), owner, id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request, owner int64) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		errorResponse(w, 400, "malformed query string")
		return
	}
	for key, values := range q {
		if (key != "status" && key != "limit" && key != "offset") || len(values) != 1 {
			errorResponse(w, 400, "unknown or repeated query parameter")
			return
		}
	}
	limit, offset := 20, 0
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			errorResponse(w, 400, "invalid limit")
			return
		}
	}
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			errorResponse(w, 400, "invalid offset")
			return
		}
	}
	tasks, err := a.service.ListTasks(r.Context(), owner, model.TaskFilter{Status: q.Get("status"), Limit: limit, Offset: offset})
	if err != nil {
		a.fail(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		Items  []model.Task `json:"items"`
		Limit  int          `json:"limit"`
		Offset int          `json:"offset"`
	}{tasks, limit, offset})
}

func taskID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		errorResponse(w, 400, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (a *API) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrValidation):
		errorResponse(w, 400, err.Error())
	case errors.Is(err, service.ErrCredentials):
		errorResponse(w, 401, "invalid email or password")
	case errors.Is(err, store.ErrEmailTaken):
		errorResponse(w, 409, "email already registered")
	case errors.Is(err, store.ErrNotFound):
		errorResponse(w, 404, "task not found")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		errorResponse(w, 503, "request could not be completed")
	default:
		a.log.Error("request failed", "error", err)
		errorResponse(w, 500, "internal server error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(dst)
	if err == nil {
		err = d.Decode(&struct{}{})
		if err == io.EOF {
			return true
		}
	}
	var large *http.MaxBytesError
	if errors.As(err, &large) {
		errorResponse(w, 413, "body exceeds 64 KiB")
	} else {
		errorResponse(w, 400, "expected one JSON object with known fields")
	}
	return false
}

func errorResponse(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// A disconnected client cannot receive another error response.
	_ = json.NewEncoder(w).Encode(value)
}
