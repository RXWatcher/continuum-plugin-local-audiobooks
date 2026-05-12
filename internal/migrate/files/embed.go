// Package files embeds the SQL migration files.
package files

import "embed"

//go:embed *.sql
var FS embed.FS
