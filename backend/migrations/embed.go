package migrations

import "embed"

// Files contains the forward-only database migrations. Embedding migrations is
// independent of the web frontend and keeps server deployments reproducible.
//
//go:embed *.sql
var Files embed.FS
