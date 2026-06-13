package migrations

import "embed"

// Files is the canonical embedded set of forward SQL migrations.
//
//go:embed *.up.sql
var Files embed.FS
