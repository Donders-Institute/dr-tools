package repocli

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/Donders-Institute/dr-tools/external/cobrashell"
	"github.com/Donders-Institute/dr-tools/internal/cmd/version"
	"github.com/Donders-Institute/tg-toolset-golang/pkg/config"
	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
	ustr "github.com/Donders-Institute/tg-toolset-golang/pkg/strings"
	"github.com/c-bata/go-prompt"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	dav "github.com/studio-b12/gowebdav"
)

var verbose bool
var configFile string
var nthreads int

var silent bool

var shellMode bool

var davBaseURL string

var cfg log.Configuration

var cli *dav.Client

// current working directory
var cwd string = "/"

var lcwd string

var rootCmd = New()

func init() {

	// current user
	user, err := user.Current()
	if err != nil {
		log.Fatalf(err.Error())
	}

	// current working directory at local
	lcwd, err = os.Getwd()
	if err != nil {
		log.Fatalf(err.Error())
	}

	// additional persistent flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", filepath.Join(user.HomeDir, ".repocli.yml"), "`path` of the configuration YAML file.")
	rootCmd.PersistentFlags().StringVarP(
		&davBaseURL,
		"url", "u", davBaseURL,
		"`URL` of the webdav server.",
	)
	// let the flag `--url` override the `repository.baseurl` in the config file.
	viper.BindPFlag("repository.baseurl", rootCmd.PersistentFlags().Lookup("url"))

	// subcommand for entering interactive shell prompt
	shellCmd := cobrashell.New(
		rootCmd,
		New,
		prompt.OptionSuggestionBGColor(prompt.DarkGray),
		prompt.OptionSuggestionTextColor(prompt.LightGray),
		prompt.OptionDescriptionBGColor(prompt.LightGray),
		prompt.OptionDescriptionTextColor(prompt.DarkGray),
		prompt.OptionSelectedDescriptionTextColor(prompt.Black),
		prompt.OptionSelectedDescriptionBGColor(prompt.Blue),
		prompt.OptionSelectedSuggestionTextColor(prompt.Black),
		prompt.OptionSelectedSuggestionBGColor(prompt.Blue),
		prompt.OptionScrollbarBGColor(prompt.Blue),
		prompt.OptionScrollbarThumbColor(prompt.DarkGray),
	)
	shellCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		shellMode = true
		// enable subcommands that make sense in interactive shell
		rootCmd.AddCommand(configCmd, loginCmd, logoutCmd, cdCmd, pwdCmd, lcdCmd, lpwdCmd, llsCmd())
		return initDavClient(shellMode)
	}
	rootCmd.AddCommand(shellCmd)

	// initiate default logger
	cfg = log.Configuration{
		EnableConsole:     true,
		ConsoleJSONFormat: false,
		ConsoleLevel:      log.Info,
	}
	log.NewLogger(cfg, log.InstanceLogrusLogger)
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "repocli",
		Short:        "A CLI for managing data content of the Donders Repository collections.",
		Long:         ``,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {

			// reset logger level based on command flag
			if cmd.Flags().Lookup("verbose").Value.String() == "true" {
				cfg.ConsoleLevel = log.Debug
			} else {
				cfg.ConsoleLevel = log.Info
			}
			log.NewLogger(cfg, log.InstanceLogrusLogger)

			// skip initializing WebDAV Client for config command
			if cmd.Use == configCmd.Use {
				return nil
			}

			return initDavClient(!shellMode)
		},
	}

	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	cmd.PersistentFlags().IntVarP(&nthreads, "nthreads", "n", 4, "`number` of concurrent worker threads.")
	cmd.PersistentFlags().BoolVarP(&silent, "silent", "s", false, "set to slient mode (i.e. do not show progress)")

	if shellMode {
		cmd.AddCommand(loginCmd, logoutCmd, cdCmd, pwdCmd, lcdCmd, lpwdCmd, llsCmd())
	}

	cmd.AddCommand(versionCmd, lsCmd(), putCmd(), getCmd(), mgetCmd(), mputCmd(), rmCmd(), mvCmd(), cpCmd(), mkdirCmd, configCmd)

	return cmd
}

// initDavClient initialize the DavClient instance
func initDavClient(prompt bool) error {

	cfg, err := filepath.Abs(configFile)
	if err != nil {
		return fmt.Errorf("cannot resolve config path: %s", configFile)
	}
	_, err = os.Stat(cfg)
	if os.IsNotExist(err) {
		if !prompt {
			log.Warnf("configuration file doesn't exist: %s, run `repocli config` first", configFile)
			return err
		}
		return promptConfig(true, true)
	}

	// load configuration file when it exists
	c, err := config.LoadConfig(configFile)
	if err != nil {
		return err
	}

	repoCfg := c.Repository
	repoUser := repoCfg.Username
	repoPass, _ := decryptPass(repoUser, repoCfg.Password)
	baseURL := repoCfg.BaseURL

	if cli == nil || (baseURL != "" && baseURL != davBaseURL) {
		davBaseURL = baseURL
		cli = newDavClient(davBaseURL, repoUser, repoPass)
	}
	return nil
}

func newDavClient(url, username, password string) *dav.Client {
	if username == "" || password == "" {
		log.Debugf("connect to %s with Empty authentication", url)
		return dav.NewAuthClient(url, dav.NewEmptyAuth())
	}
	log.Debugf("connect to %s with with BasicAuth authentication", url)
	return dav.NewAuthClient(url, dav.NewPreemptiveAuth(&BasicAuth{user: username, pw: password}))
}

// versionCmd prints out the version number of the package.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "print version number and exit",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("repocli version: %s\n", version.Version)
	},
}

// decryptPass decrypts the hex string back to the plaintext password
func decryptPass(username, shex string) (string, error) {
	p, _ := filepath.Abs(configFile)
	k := ustr.MD5Encode(fmt.Sprintf("%s.%s", p, username))

	bpass, err := hex.DecodeString(shex)
	if err != nil {
		return "", err
	}

	pass, err := ustr.Decrypt(bpass, []byte(k))
	if err != nil {
		return "", err
	}

	return string(pass), nil
}

// Execute is the main entry point of the cluster command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
