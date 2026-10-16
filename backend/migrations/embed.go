// Package migrations встраивает SQL-миграции goose в бинарь, чтобы схема
// всегда ехала вместе с кодом, который на неё рассчитан.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
