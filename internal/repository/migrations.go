package repository

import (
	"errors"
	"fmt"

	migrationsfs "github.com/AGubenskiy/metrics/migrations"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

func ApplyMigrations(databaseDSN string) error {
	sourceDriver, err := iofs.New(migrationsfs.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", sourceDriver, databaseDSN)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	upErr := migrator.Up()
	sourceErr, databaseErr := migrator.Close()
	closeErr := errors.Join(sourceErr, databaseErr)
	if upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
		return errors.Join(fmt.Errorf("apply migrations: %w", upErr), closeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close migrator: %w", closeErr)
	}

	return nil
}
