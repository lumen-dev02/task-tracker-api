package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/lumen-dev02/task-tracker-api/internal/model"
	"github.com/lumen-dev02/task-tracker-api/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var ErrValidation = errors.New("validation error")
var ErrCredentials = errors.New("invalid email or password")

// Repository describes only operations needed by this service; Postgres is the
// production implementation and tests can provide a small in-memory double.
type Repository interface {
	CreateUser(context.Context, string, string, string) (model.User, error)
	UserByEmail(context.Context, string) (model.User, error)
	CreateTask(context.Context, int64, model.TaskInput) (model.Task, error)
	GetTask(context.Context, int64, int64) (model.Task, error)
	UpdateTask(context.Context, int64, int64, model.TaskInput) (model.Task, error)
	DeleteTask(context.Context, int64, int64) error
	ListTasks(context.Context, int64, model.TaskFilter) ([]model.Task, error)
}

type Service struct {
	repo      Repository
	dummyHash []byte
}

func New(repo Repository) (*Service, error) {
	// Unknown emails still incur one bcrypt comparison, like wrong passwords.
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-not-a-user"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, dummyHash: dummy}, nil
}

func normalizeEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 {
		return "", false
	}
	parsed, err := mail.ParseAddress(email)
	return email, err == nil && parsed.Address == email && parsed.Name == "" && strings.Contains(email, "@")
}

func (s *Service) Register(ctx context.Context, email, name, password string) (model.User, error) {
	email, valid := normalizeEmail(email)
	name = strings.TrimSpace(name)
	if !valid || len(name) < 1 || len(name) > 80 || strings.ContainsRune(name, 0) || len(password) < 12 || len(password) > 72 {
		return model.User{}, fmt.Errorf("%w: valid email, name 1..80 bytes and password 12..72 bytes are required", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}
	return s.repo.CreateUser(ctx, email, name, string(hash))
}

func (s *Service) Login(ctx context.Context, email, password string) (model.User, error) {
	email, valid := normalizeEmail(email)
	if !valid || len(password) < 12 || len(password) > 72 {
		return model.User{}, ErrCredentials
	}
	u, err := s.repo.UserByEmail(ctx, email)
	if errors.Is(err, store.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return model.User{}, ErrCredentials
	}
	if err != nil {
		return model.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return model.User{}, ErrCredentials
	}
	return u, nil
}

func validateTask(in model.TaskInput) (model.TaskInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Title) < 1 || len(in.Title) > 200 || len(in.Description) > 10000 || strings.ContainsRune(in.Title, 0) || strings.ContainsRune(in.Description, 0) || !model.ValidStatus(in.Status) {
		return model.TaskInput{}, fmt.Errorf("%w: title must be 1..200 bytes, description <=10000 bytes, status one of todo, in_progress, done", ErrValidation)
	}
	return in, nil
}

func (s *Service) CreateTask(ctx context.Context, owner int64, in model.TaskInput) (model.Task, error) {
	if in.Status == "" {
		in.Status = "todo"
	}
	in, err := validateTask(in)
	if err != nil {
		return model.Task{}, err
	}
	return s.repo.CreateTask(ctx, owner, in)
}

func (s *Service) UpdateTask(ctx context.Context, owner, id int64, in model.TaskInput) (model.Task, error) {
	in, err := validateTask(in)
	if err != nil {
		return model.Task{}, err
	}
	return s.repo.UpdateTask(ctx, owner, id, in)
}

func (s *Service) GetTask(ctx context.Context, owner, id int64) (model.Task, error) {
	return s.repo.GetTask(ctx, owner, id)
}
func (s *Service) DeleteTask(ctx context.Context, owner, id int64) error {
	return s.repo.DeleteTask(ctx, owner, id)
}

func (s *Service) ListTasks(ctx context.Context, owner int64, f model.TaskFilter) ([]model.Task, error) {
	if (f.Status != "" && !model.ValidStatus(f.Status)) || f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 100000 {
		return nil, fmt.Errorf("%w: invalid status or pagination (limit 1..100, offset 0..100000)", ErrValidation)
	}
	return s.repo.ListTasks(ctx, owner, f)
}
