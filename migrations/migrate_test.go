package migrations

import (
	"context"
	"sync"
	"testing"

	"github.com/lumen-dev02/task-tracker-api/internal/testutil"
)

func TestConcurrentApply(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Apply(ctx, pool); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("versions = %d, %v", n, err)
	}
}

func TestMigrationRollback(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE tasks (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, pool); err == nil {
		t.Fatal("expected conflicting table to fail migration")
	}
	var usersMissing, versionsMissing bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('users') IS NULL, to_regclass('schema_migrations') IS NULL`).Scan(&usersMissing, &versionsMissing); err != nil {
		t.Fatal(err)
	}
	if !usersMissing || !versionsMissing {
		t.Fatal("failed migration left partial changes")
	}
	if _, err := pool.Exec(ctx, `DROP TABLE tasks`); err != nil {
		t.Fatal(err)
	}
	if err := Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
