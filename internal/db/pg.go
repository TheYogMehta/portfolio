package db

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

var DB *sql.DB

func InitDB(connStr string) (*sql.DB, error) {
	dbConn, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	if err := dbConn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	if err := goose.Up(dbConn, "internal/db/migrations"); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	DB = dbConn
	return dbConn, nil
}
