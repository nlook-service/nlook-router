package cli

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/nlook-service/nlook-router/internal/config"
	"github.com/nlook-service/nlook-router/internal/db"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database management commands",
}

var dbMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate JSON files to SQLite database",
	RunE: func(cmd *cobra.Command, args []string) error {
		dataDir := config.ConfigDir()
		cfg, err := config.Load(GetConfigPath())
		if err == nil && cfg.DB.DataDir != "" {
			dataDir = cfg.DB.DataDir
		}

		fmt.Printf("Migrating JSON files from %s to SQLite...\n", dataDir)
		if err := db.MigrateFileToSQLite(dataDir); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		fmt.Println("Migration completed successfully.")
		fmt.Println("To use SQLite, add to config.yaml:")
		fmt.Println("  db:")
		fmt.Println("    driver: sqlite")
		return nil
	},
}

var dbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show database status and statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(GetConfigPath())
		if err != nil {
			return err
		}

		dataDir := config.ConfigDir()
		if cfg.DB.DataDir != "" {
			dataDir = cfg.DB.DataDir
		}
		driver := cfg.DB.Driver
		if driver == "" {
			driver = "file"
		}

		fmt.Printf("Driver:   %s\n", driver)
		fmt.Printf("Data Dir: %s\n", dataDir)

		// Check file sizes
		files := []string{"memory.json", "cache.json", "sessions.json", "nlook.db"}
		for _, f := range files {
			path := dataDir + "/" + f
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			fmt.Printf("  %s: %s\n", f, humanSize(info.Size()))
		}

		// If sqlite, show row counts
		if driver == "sqlite" {
			storage, err := db.New("sqlite", dataDir)
			if err != nil {
				log.Printf("open sqlite: %v", err)
				return nil
			}
			defer storage.Close()
			fmt.Println("(SQLite row counts require direct query - use sqlite3 CLI)")
		}

		return nil
	},
}

func init() {
	dbCmd.AddCommand(dbMigrateCmd)
	dbCmd.AddCommand(dbStatusCmd)
	rootCmd.AddCommand(dbCmd)
}

