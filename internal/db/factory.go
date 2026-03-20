package db

import (
	"fmt"
	"path/filepath"
)

// New creates a DB instance based on the driver name.
// Supported drivers: "file" (default), "sqlite".
func New(driver string, dataDir string) (DB, error) {
	switch driver {
	case "file", "":
		return newFileDB(dataDir)
	case "sqlite":
		path := filepath.Join(dataDir, "nlook.db")
		return newSQLiteDB(path)
	default:
		return nil, fmt.Errorf("unsupported db driver: %q (supported: file, sqlite)", driver)
	}
}
