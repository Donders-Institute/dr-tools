package repocli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"encoding/hex"
	"syscall"
	"time"

	"github.com/Donders-Institute/tg-toolset-golang/pkg/config"
	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
	ustr "github.com/Donders-Institute/tg-toolset-golang/pkg/strings"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	dav "github.com/studio-b12/gowebdav"
	"golang.org/x/term"
	"gopkg.in/yaml.v2"
)

// command to change directory in the repository.
// This command only makes sense in shell mode.
var cdCmd = &cobra.Command{
	Use:   "cd <repo_dir>",
	Short: "change present working directory in the repository",
	Long:  ``,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		p := getCleanRepoPath(args[0])

		// stat the path to check if the path is a valid directory
		if f, err := cli.Stat(p); err != nil || !f.IsDir() {
			return fmt.Errorf("invalid directory: %s", p)
		}

		// set cwd to the new path
		cwd = p
		return nil
	},
	ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// get list of content in this directory
		if len(args) == 0 {
			p := cwd
			if toComplete != "" {
				p = toComplete
			}
			return append([]string{".", ".."}, getContentNamesRepo(p, true)...), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveError
	},
}

// command to show present working directory in the repository.
// This command only makes sense in shell mode.
var pwdCmd = &cobra.Command{
	Use:   "pwd",
	Short: "print present working directory in the repository",
	Long:  ``,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("%s\n", cwd)
		return nil
	},
}

// command to show present working directory at local.
// This command only makes sense in shell mode.
var lcdCmd = &cobra.Command{
	Use:   "lcd",
	Short: "change present working directory at local",
	Long:  ``,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := filepath.Abs(args[0])

		if err != nil {
			return err
		}

		if err := os.Chdir(p); err != nil {
			return err
		}

		lcwd = p
		return nil
	},
	ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// get list of content in this directory
		if len(args) == 0 {
			p := lcwd
			if toComplete != "" {
				p = toComplete
			}
			return append([]string{".", ".."}, getContentNamesLocal(p, true)...), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveError
	},
}

// command to show present working directory at local.
// This command only makes sense in shell mode.
var lpwdCmd = &cobra.Command{
	Use:   "lpwd",
	Short: "print present working directory at local",
	Long:  ``,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("%s\n", lcwd)
		return nil
	},
}

// command to show content of a local directory.
// This command only makes sense in shell mode.
func llsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lls",
		Short: "list files or directories at local",
		Long: `
The "lls" subcommand is for listing files and directories at local, with wildcard support.
		`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			p := lcwd
			var err error
			if len(args) == 1 {
				p, err = filepath.Abs(args[0])
				if err != nil {
					return err
				}
			}

			files := make([]fs.FileInfo, 0)
			if f, err := os.Stat(p); err == nil {
				if !f.IsDir() {
					// user input is a file
					files = append(files, f)
				} else {
					// user input is a directory
					entries, err := os.ReadDir(p)
					if err != nil {
						return err
					}
					for _, entry := range entries {
						if info, err := entry.Info(); err != nil {
							log.Errorf("%s: %s", err, entry.Name())
						} else {
							files = append(files, info)
						}
					}
				}
			} else if errors.Is(err, os.ErrNotExist) && len(args) == 1 {
				// assuming the user input is a wildcard
				p = filepath.Dir(p)
				if matches, err := filepath.Glob(args[0]); err == nil && matches != nil {
					for _, m := range matches {
						if f, err := os.Stat(m); err == nil {
							files = append(files, f)
						}
					}
				}
			}

			if longformat {
				for _, f := range files {
					fmt.Printf("%11s %12d %s %s\n", f.Mode(), f.Size(), f.ModTime().Format(time.UnixDate), filepath.Join(p, f.Name()))
				}
			} else {
				isDirMarker := make(map[bool]rune, 2)
				isDirMarker[true] = '/'
				isDirMarker[false] = 0

				for _, f := range files {
					fmt.Printf("%s%c\n", f.Name(), isDirMarker[f.Mode().IsDir()])
				}
			}
			return nil
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if len(args) == 0 {
				p := lcwd
				if toComplete != "" {
					p = toComplete
				}
				return append([]string{".", ".."}, getContentNamesLocal(p, false)...), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}

	cmd.Flags().BoolVarP(&longformat, "long", "l", false, "list files with more detail")

	return cmd
}

// command to login repository
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "login the repository with the data-access account",
	Long:  ``,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return promptLogin()
	},
}

// getContentNamesInLocalDir get a lists of entry names in the local directory.
// it returns an empty list in case of error.
func getContentNamesLocal(path string, dirOnly bool) []string {
	names := make([]string, 0)
	if entries, err := os.ReadDir(path); err == nil {
		for _, entry := range entries {
			if finfo, err := entry.Info(); err == nil && (!dirOnly || finfo.IsDir()) {
				names = append(names, finfo.Name())
			}
		}
	}
	return names
}

// getContentNamesInLocalDir get a lists of entry names in the local directory.
func getContentNamesRepo(path string, dirOnly bool) []string {
	names := make([]string, 0)
	if entries, err := cli.ReadDir(getCleanRepoPath(path)); err == nil {
		for _, finfo := range entries {
			if !dirOnly || finfo.IsDir() {
				names = append(names, finfo.Name())
			}
		}
	}
	return names
}

// promptLogin asks username and password input for
// authenticating to the webdav interface.
func promptLogin() error {

	// prompt for baseurl if it is not set in current shell
	if davBaseURL == "" {
		davBaseURL = stringPrompt("repo baseurl")
	}

	fmt.Fprintf(os.Stderr, "login for %s\n", davBaseURL)

	repoUser := stringPrompt("username")
	repoPass := passwordPrompt("password")

	save := boolPrompt("save credential")

	// try to connect the repo webdav to check authentication
	cli = dav.NewClient(davBaseURL, repoUser, repoPass)
	if err := cli.Connect(); err != nil {
		return err
	}

	// save credential to `configFile`
	if save {
		return saveCredential(davBaseURL, repoUser, repoPass)
	}

	return nil
}

// saveCredential saves the username/password to the file `configFile` with file mode 600.
func saveCredential(baseURL, username, password string) error {

	// encrypt password before saving to the file
	p, _ := filepath.Abs(configFile)
	k := ustr.MD5Encode(fmt.Sprintf("%s.%s", p, username))
	epass, err := ustr.Encrypt([]byte(password), []byte(k))
	if err != nil {
		return err
	}

	conf, err := yaml.Marshal(&struct {
		Repository config.RepositoryConfiguration `yaml:"repository"`
	}{
		config.RepositoryConfiguration{
			BaseURL:  baseURL,
			Username: username,
			Password: hex.EncodeToString(epass),
		},
	})

	if err != nil {
		return err
	}

	vconf := viper.New()
	vconf.SetConfigType("yaml")
	err = vconf.ReadConfig(bytes.NewBuffer(conf))
	if err != nil {
		return err
	}

	if err := vconf.WriteConfigAs(configFile); err != nil {
		return err
	}

	if err := os.Chmod(configFile, 0600); err != nil {
		return err
	}

	log.Infof("credential saved in %s", configFile)

	return nil
}

// boolPrompt asks for a string value `y/n` and return a boolean accordingly.
func boolPrompt(label string) bool {
	var s string
	fmt.Fprintf(os.Stderr, label+" [y/N]: ")
	fmt.Scanf("%s\n", &s)

	if s == "y" || s == "Y" {
		return true
	}
	return false
}

// stringPrompt asks for a string value using the label
func stringPrompt(label string) string {
	var s string
	fmt.Fprintf(os.Stderr, label+": ")
	fmt.Scanf("%s\n", &s)
	return s
}

// passwordPrompt asks for a password value using the label
func passwordPrompt(label string) string {
	var s string
	for {
		fmt.Fprint(os.Stderr, label+": ")
		b, _ := term.ReadPassword(int(syscall.Stdin))
		s = string(b)
		if s != "" {
			break
		}
	}
	fmt.Println()
	return s
}
