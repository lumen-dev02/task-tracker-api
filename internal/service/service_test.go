package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lumen-dev02/task-tracker-api/internal/model"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
	"golang.org/x/crypto/bcrypt"
)

type fakeRepo struct {
	Repository
	user  model.User
	err   error
	input model.TaskInput
	owner int64
	id    int64
}

func (f *fakeRepo) CreateUser(_ context.Context, email, name, hash string) (model.User, error) {
	f.user = model.User{ID: 1, Email: email, Name: name, PasswordHash: hash}
	return f.user, f.err
}
func (f *fakeRepo) UserByEmail(context.Context, string) (model.User, error) { return f.user, f.err }
func (f *fakeRepo) CreateTask(_ context.Context, owner int64, in model.TaskInput) (model.Task, error) {
	f.owner, f.input = owner, in
	return model.Task{ID: 1, OwnerID: owner, Title: in.Title, Status: in.Status}, f.err
}
func (f *fakeRepo) UpdateTask(ctx context.Context, owner, id int64, in model.TaskInput) (model.Task, error) {
	f.id = id
	return f.CreateTask(ctx, owner, in)
}
func (f *fakeRepo) ListTasks(_ context.Context, owner int64, _ model.TaskFilter) ([]model.Task, error) {
	f.owner = owner
	return []model.Task{}, f.err
}

func TestRegisterAndLogin(t *testing.T) {
	f := &fakeRepo{}
	s, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const password = "correct-horse-battery"
	u, err := s.Register(ctx, "  USER@Example.com ", " Timur ", password)
	if err != nil || u.Email != "user@example.com" || u.Name != "Timur" {
		t.Fatalf("Register = %+v, %v", u, err)
	}
	if f.user.PasswordHash == password || bcrypt.CompareHashAndPassword([]byte(f.user.PasswordHash), []byte(password)) != nil {
		t.Fatal("password was not hashed correctly")
	}
	if _, err := s.Login(ctx, "USER@example.com", password); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, u.Email, "wrong-password"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	f.err = store.ErrNotFound
	if _, err := s.Login(ctx, u.Email, password); !errors.Is(err, ErrCredentials) {
		t.Fatalf("unknown email: %v", err)
	}
	f.err = errors.New("database down")
	if _, err := s.Login(ctx, u.Email, password); !errors.Is(err, f.err) {
		t.Fatalf("database error hidden: %v", err)
	}
	f.err = store.ErrEmailTaken
	if _, err := s.Register(ctx, u.Email, u.Name, password); !errors.Is(err, store.ErrEmailTaken) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestRegistrationValidation(t *testing.T) {
	s, err := New(nil) // Invalid input must never reach the repository.
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ email, name, password string }{
		{"invalid", "Timur", "long-password"},
		{"Name <user@example.com>", "Timur", "long-password"},
		{strings.Repeat("a", 255) + "@example.com", "Timur", "long-password"},
		{"user@example.com", " ", "long-password"},
		{"user@example.com", "bad\x00name", "long-password"},
		{"user@example.com", strings.Repeat("я", 41), "long-password"},
		{"user@example.com", "Timur", "short"},
		{"user@example.com", "Timur", strings.Repeat("x", 73)},
	} {
		if _, err := s.Register(context.Background(), tc.email, tc.name, tc.password); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if _, err := s.Login(context.Background(), "bad", "long-password"); !errors.Is(err, ErrCredentials) {
		t.Fatal(err)
	}
}

func TestTaskValidationAndOwnerPropagation(t *testing.T) {
	f := &fakeRepo{}
	s, err := New(f)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	j, err := s.CreateTask(ctx, 42, model.TaskInput{Title: " Write tests "})
	if err != nil || j.Status != "todo" || f.owner != 42 || f.input.Title != "Write tests" {
		t.Fatalf("Create = %+v, %v", j, err)
	}
	for _, status := range []string{"todo", "in_progress", "done"} {
		if _, err := s.UpdateTask(ctx, 42, 7, model.TaskInput{Title: "Task", Status: status}); err != nil {
			t.Fatal(err)
		}
		if f.id != 7 || f.owner != 42 {
			t.Fatal("owner or task ID lost")
		}
	}
	for _, in := range []model.TaskInput{
		{Title: "bad\x00title"}, {Title: "Task", Description: "bad\x00description"},
		{Title: " "}, {Title: strings.Repeat("x", 201)}, {Title: "Task", Status: "unknown"}, {Title: "Task", Description: strings.Repeat("x", 10001)},
	} {
		if _, err := s.CreateTask(ctx, 1, in); !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted %+v: %v", in, err)
		}
	}
	if _, err := s.UpdateTask(ctx, 42, 7, model.TaskInput{Title: "Task"}); !errors.Is(err, ErrValidation) {
		t.Fatal("PUT accepted missing status")
	}
	for _, f := range []model.TaskFilter{{Limit: 0}, {Limit: 101}, {Limit: 20, Offset: -1}, {Limit: 20, Offset: 100001}, {Limit: 20, Status: "bad"}} {
		if _, err := s.ListTasks(ctx, 42, f); !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted filter %+v", f)
		}
	}
	if _, err := s.ListTasks(ctx, 42, model.TaskFilter{Limit: 20, Status: "done"}); err != nil {
		t.Fatal(err)
	}
}
