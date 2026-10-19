package driver

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type postgres struct{}

func init() { Register(postgres{}) }

func (postgres) Dialect() string          { return "postgres" }
func (postgres) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (postgres) Open(ctx context.Context, dsn string) (*sql.DB, error) {
	return open(ctx, "pgx", dsn)
}
