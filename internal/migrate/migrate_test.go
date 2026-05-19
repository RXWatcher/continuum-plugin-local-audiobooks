package migrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0002Applies(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("LOCAL_AUDIOBOOKS_TEST_DSN")
	if dsn == "" {
		t.Skip("LOCAL_AUDIOBOOKS_TEST_DSN unset; skipping integration migration test")
	}
	if err := Run(ctx, dsn); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
        WHERE table_name IN ('metadata_cache','metadata_enrichment_job','app_config')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 configured tables, found %d", n)
	}
}
