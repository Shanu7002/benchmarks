package driver

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

type mysql struct{}

func init() { Register(mysql{}) }

func (mysql) Dialect() string        { return "mysql" }
func (mysql) Placeholder(int) string { return "?" }
func (mysql) Open(ctx context.Context, dsn string) (*sql.DB, error) {
	return open(ctx, "mysql", dsn)
}
