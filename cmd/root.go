package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/linkedin"
	"linkedin-jobs/internal/store"
)

var (
	cfgFlag dbFlag
	jsonOut bool
)

// dbFlag stores the optional --db path override.
type dbFlag struct{ path string }

var rootCmd = &cobra.Command{
	Use:   "linkedin-jobs",
	Short: "LinkedIn jobs CLI — recommended jobs from your session, search, filter, summarize, store.",
	Long: `linkedin-jobs pulls your personalized "Recommended for you" job feed from your
LinkedIn session, searches the public job board, parses salaries, summarizes
postings with an LLM, and persists everything to a local SQLite store with
offline full-text search.

Recommended jobs (the headline command) require a logged-in session:
    linkedin-jobs auth login       # capture your session via press-auth
    linkedin-jobs recommended      # pull your personalized feed

Anonymous search works without a session:
    linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k`,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFlag.path, "db", "", "path to the SQLite DB file (default: ./linkedin_jobs.db or $LJ_DB_PATH)")
	// --json is added per-command where supported, but we also expose a global.
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON (agent-native output)")
}

func loadCfg() config.Config {
	cfg := config.Load()
	if cfgFlag.path != "" {
		cfg = cfg.WithDBPath(cfgFlag.path)
	}
	return cfg
}

// openStore opens (creating) the database from the resolved config.
func openStore() (*store.Store, error) {
	return store.Open(loadCfg().DBPath)
}

// newClient builds a LinkedIn client; withSession controls whether an
// authenticated session is attached.
func newClient(withSession bool) (*linkedin.Client, error) {
	cfg := loadCfg()
	c := linkedin.New(cfg)
	if withSession {
		if _, err := attachSession(c); err != nil {
			return c, err
		}
	}
	return c, nil
}

// die prints an error to stderr and exits 1.
func die(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
