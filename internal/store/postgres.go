package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lumen-dev02/task-tracker-api/internal/model"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrEmailTaken = errors.New("email already registered")
)

type Postgres struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (s *Postgres) CreateUser(ctx context.Context, email, name, hash string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx, `INSERT INTO users(email, name, password_hash) VALUES ($1, $2, $3)
		RETURNING id, email, name, created_at`, email, name, hash).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return model.User{}, ErrEmailTaken
	}
	return u, err
}

func (s *Postgres) UserByEmail(ctx context.Context, email string) (model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx, `SELECT id, email, name, password_hash, created_at FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	return u, translate(err)
}

const taskColumns = `id, owner_id, title, description, status, created_at, updated_at`

func scanTask(row pgx.Row) (model.Task, error) {
	var t model.Task
	err := row.Scan(&t.ID, &t.OwnerID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, translate(err)
}

func (s *Postgres) CreateTask(ctx context.Context, ownerID int64, in model.TaskInput) (model.Task, error) {
	return scanTask(s.pool.QueryRow(ctx, `INSERT INTO tasks(owner_id, title, description, status) VALUES ($1, $2, $3, $4) RETURNING `+taskColumns,
		ownerID, in.Title, in.Description, in.Status))
}

// Owner filtering is part of every query, not a separate check before a write.
func (s *Postgres) GetTask(ctx context.Context, ownerID, id int64) (model.Task, error) {
	return scanTask(s.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE owner_id = $1 AND id = $2`, ownerID, id))
}

func (s *Postgres) UpdateTask(ctx context.Context, ownerID, id int64, in model.TaskInput) (model.Task, error) {
	return scanTask(s.pool.QueryRow(ctx, `UPDATE tasks SET title = $3, description = $4, status = $5, updated_at = clock_timestamp()
		WHERE owner_id = $1 AND id = $2 RETURNING `+taskColumns, ownerID, id, in.Title, in.Description, in.Status))
}

func (s *Postgres) DeleteTask(ctx context.Context, ownerID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tasks WHERE owner_id = $1 AND id = $2`, ownerID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Postgres) ListTasks(ctx context.Context, ownerID int64, f model.TaskFilter) ([]model.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE owner_id = $1`
	args := []any{ownerID, f.Limit, f.Offset}
	if f.Status != "" {
		query += ` AND status = $4`
		args = append(args, f.Status)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := make([]model.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
