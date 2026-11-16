package migrate

import (
	"embed"
	"io/fs"
	"log"

	atlasmigrate "ariga.io/atlas/sql/migrate"
)

//go:embed migrations/*.sql migrations/atlas.sum
var migrations embed.FS

// Dir is the Atlas migration directory used by the migrate binary.
var Dir atlasmigrate.Dir

func init() {
	var err error
	// The local folder first: it is there in development and when migrations are generated.
	Dir, err = atlasmigrate.NewLocalDir("internal/ent/migrate/migrations")
	if err == nil {
		return
	}
	// The embedded copy in the container image, where the source tree is absent.
	Dir, err = newEmbedDir(migrations, "migrations")
	if err != nil {
		log.Fatalf("open embedded migration directory: %v", err)
	}
}

// newEmbedDir builds an in-memory Atlas directory from an embed.FS sub-path.
func newEmbedDir(fsys embed.FS, path string) (*atlasmigrate.MemDir, error) {
	sub, err := fs.Sub(fsys, path)
	if err != nil {
		return nil, err
	}
	md := atlasmigrate.OpenMemDir(path)
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(sub, e.Name())
		if err != nil {
			return nil, err
		}
		if err := md.WriteFile(e.Name(), data); err != nil {
			return nil, err
		}
	}
	return md, nil
}
