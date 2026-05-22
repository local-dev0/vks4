package app

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrationsPath — путь, по которому миграции лежат в контейнере.
// На dev можно переопределить через ENV MIGRATIONS_PATH.
const MigrationsPath = "/app/migrations"

func newMigrator(dbURL, path string) (*migrate.Migrate, error) {
	if path == "" {
		path = MigrationsPath
	}
	m, err := migrate.New("file://"+path, dbURL)
	if err != nil {
		return nil, fmt.Errorf("init migrate: %w", err)
	}
	return m, nil
}

func MigrateUp(dbURL, path string) error {
	m, err := newMigrator(dbURL, path)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && err.Error() != "no change" {
		return err
	}
	return nil
}

func MigrateDown(dbURL, path string) error {
	m, err := newMigrator(dbURL, path)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Down(); err != nil && err.Error() != "no change" {
		return err
	}
	return nil
}
