package migrate

import (
	"embed"
	"log"

	atlasmigrate "ariga.io/atlas/sql/migrate"
)

//go:embed migrations/*.sql migrations/atlas.sum
var migrations embed.FS

// Dir is the Atlas migration directory used by the runtime migrate binary.
var Dir atlasmigrate.Dir

func init() {
	var err error
	Dir, err = atlasmigrate.NewLocalDir("internal/ent/migrate/migrations")
	if err != nil {
		log.Printf("Warning: failed to open Atlas migration dir: %v", err)
	}
}
