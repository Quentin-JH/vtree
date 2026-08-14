// Package db provisions and removes per-tree MySQL schemas through the Go
// driver — no mysql client binary required on the machine.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Quentin-JH/vtree/internal/config"
)

// schemaIdent is the shape naming.SchemaName produces. Enforced again here so
// nothing interpolated into DDL can ever be anything else.
var schemaIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func Connect(loc *config.LocalDatabase) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=5s", loc.User, loc.Password, loc.Host, loc.Port)
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("cannot reach MySQL at %s:%d as %s: %w", loc.Host, loc.Port, loc.User, err)
	}
	return conn, nil
}

func EnsureSchema(conn *sql.DB, name string) error {
	if !schemaIdent.MatchString(name) {
		return fmt.Errorf("refusing to create schema with unexpected name %q", name)
	}
	_, err := conn.Exec("CREATE DATABASE IF NOT EXISTS `" + name + "`")
	return err
}

func DropSchema(conn *sql.DB, name string) error {
	if !schemaIdent.MatchString(name) {
		return fmt.Errorf("refusing to drop schema with unexpected name %q", name)
	}
	_, err := conn.Exec("DROP DATABASE IF EXISTS `" + name + "`")
	return err
}
