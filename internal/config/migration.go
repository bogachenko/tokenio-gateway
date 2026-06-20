package config

import (
	"fmt"
	"strings"
)

type MigrationConfig struct {
	DatabaseDSN string
}

func LoadMigration() (MigrationConfig, error) {
	loader := envLoader{}
	cfg := MigrationConfig{
		DatabaseDSN: loader.required("TOKENIO_DATABASE_DSN"),
	}

	if len(loader.errs) > 0 {
		return MigrationConfig{}, fmt.Errorf(
			"invalid migration config: %s",
			strings.Join(loader.errs, "; "),
		)
	}
	return cfg, nil
}
