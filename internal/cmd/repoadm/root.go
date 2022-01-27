package repoadm

import (
	"os"

	"github.com/Donders-Institute/tg-toolset-golang/pkg/config"
	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
	"github.com/Donders-Institute/tg-toolset-golang/project/pkg/pdb"
	"github.com/spf13/cobra"
)

var verbose bool
var configFile string
var cfg log.Configuration

const (
	// RepoRootPath defines the filesystem root path of the repository data collections.
	RepoRootPath = "/.repo"
	// RepoExportPath defines the filesystem root path of the exported repository data collections.
	RepoExportPath = "/cephfs/data/export"
	// RepoNamespace is the iRODS namespace in which organisation units of the repository are located.
	RepoNamespace = "/nl.ru.donders/di"
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "config.yml", "`path` of the configuration YAML file.")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	// initiate default logger
	cfg = log.Configuration{
		EnableConsole:     true,
		ConsoleJSONFormat: false,
		ConsoleLevel:      log.Info,
	}
	log.NewLogger(cfg, log.InstanceLogrusLogger)
}

// loadConfig loads configuration YAML file specified by `configFile`.
// This function fatals out if there is an error.
func loadConfig() config.Configuration {
	conf, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("%s", err)
	}
	return conf
}

// loadPdb initializes the PDB interface package using the configuration YAML file.
// This function fatals out if there is an error.
func loadPdb() pdb.PDB {
	// initialize pdb interface
	conf := loadConfig()
	ipdb, err := pdb.New(conf.PDB)
	if err != nil {
		log.Fatalf("%s", err)
	}

	return ipdb
}

var rootCmd = &cobra.Command{
	Use:   "repoutil",
	Short: "The administrator's CLI for managing the Donders Repository",
	Long:  ``,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// reset logger level
		if cmd.Flags().Changed("verbose") {
			cfg.ConsoleLevel = log.Debug
		}
		log.NewLogger(cfg, log.InstanceLogrusLogger)
	},
}

// Execute is the main entry point of the cluster command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		log.Errorf("%s", err)
		os.Exit(1)
	}
}
