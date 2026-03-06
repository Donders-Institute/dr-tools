package repocli

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Donders-Institute/tg-toolset-golang/pkg/config"
	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
	ustr "github.com/Donders-Institute/tg-toolset-golang/pkg/strings"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
	"gopkg.in/yaml.v2"
)

// command to login webdav interactively
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "setup WebDAV client with BasicAuth authentication",
	Long: `
The "login" subcommand configures the WebDAV client to use username/password for BasicAuth authentication.
		`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		promptConfig(false, false)
		return nil
	},
}

// command to logout webdav interactively
var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "setup WebDAV client with Empty authentication",
	Long: `
The "logout" subcommand configures the WebDAV client for Empty credential.
		`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli = newDavClient(davBaseURL, "", "")
		return nil
	},
}

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
					p = filepath.Dir(p)
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

// command to config WebDAV connection
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "configure the repository connection and save the credential",
	Long:  ``,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return promptConfig(true, true)
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

// promptConfig asks username and password input for
// authenticating to the webdav interface.
func promptConfig(reset bool, saveCredential bool) error {

	var err error

	// prompt for baseurl if it is not set in current shell
	if reset || davBaseURL == "" {
		davBaseURL, err = stringPromptInterruptable("repo baseurl")
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "\rlogin for %s\n", davBaseURL)

	repoUser, err := stringPromptInterruptable("username")

	if err != nil {
		return err
	}

	repoPass, err := passwordPromptMasked("password")

	if err != nil {
		return err
	}

	// allow user to choose whether the credential should be saved in the configuration file
	if !saveCredential {
		saveCredential = boolPrompt("save credential")
	}

	// try to connect the repo webdav to check authentication
	cli = newDavClient(davBaseURL, repoUser, repoPass)

	// save to configuration file `configFile`
	return saveConfig(davBaseURL, repoUser, repoPass, saveCredential)
}

// saveConfig saves the username/password to the file `configFile` with file mode 600.
func saveConfig(baseURL, username, password string, saveCredential bool) error {

	// encrypt password before saving to the file
	p, _ := filepath.Abs(configFile)

	cfg := config.RepositoryConfiguration{
		BaseURL:  baseURL,
		Username: username,
	}

	if saveCredential {
		k := ustr.MD5Encode(fmt.Sprintf("%s.%s", p, username))
		epass, err := ustr.Encrypt([]byte(password), []byte(k))
		if err != nil {
			return err
		}
		cfg.Password = hex.EncodeToString(epass)
	}

	conf, err := yaml.Marshal(&struct {
		Repository config.RepositoryConfiguration `yaml:"repository"`
	}{
		cfg,
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

	log.Infof("\rsaved configuration in %s", configFile)
	return nil
}

// boolPrompt asks for a string value `y/n` and return a boolean accordingly.
func boolPrompt(label string) bool {
	var s string
	fmt.Fprintf(os.Stderr, "\r"+label+" [y/N]: ")
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

func stringPromptInterruptable(label string) (string, error) {

	fmt.Fprintf(os.Stderr, "\r"+label+": ")

	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, oldState)

	var input []byte
	buf := make([]byte, 1)

	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			return "", err
		}

		b := buf[0]

		switch b {

		case '\r', '\n': // Enter
			fmt.Println()
			return string(input), nil

		case 3: // Ctrl+C
			fmt.Println("\r")
			return "", errors.New("prompt interrupted")

		case 127, 8: // Backspace
			if len(input) > 0 {
				input = input[:len(input)-1]
				fmt.Fprint(os.Stderr, "\b \b")
			}

		case 27: // Escape sequence (arrow keys)
			seq := make([]byte, 2)
			os.Stdin.Read(seq) // discard

		default:
			if b >= 32 && b <= 126 {
				input = append(input, b)
				fmt.Fprintf(os.Stderr, "%c", b)
			}
		}
	}
}

// passwordPrompt asks for a password value using the label
func passwordPrompt(label string) string {
	var s string
	fmt.Fprint(os.Stderr, label+": ")
	b, _ := term.ReadPassword(int(syscall.Stdin))
	s = string(b)
	fmt.Println()
	return s
}

func passwordPromptMasked(label string) (string, error) {

	fmt.Fprint(os.Stderr, "\r"+label+": ")

	fd := int(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, oldState)

	var password []byte
	buf := make([]byte, 1)

	for {
		_, err := os.Stdin.Read(buf)
		if err != nil {
			return "", err
		}

		b := buf[0]

		switch b {

		// ENTER
		case '\r', '\n':
			fmt.Println()
			return string(password), nil

		// CTRL+C
		case 3:
			fmt.Println("\r")
			return "", errors.New("interrupted")

		// BACKSPACE
		case 127, 8:
			if len(password) > 0 {
				password = password[:len(password)-1]
				fmt.Fprint(os.Stderr, "\b \b")
			}

		// ESC sequence (arrow keys etc.)
		case 27:
			seq := make([]byte, 2)
			os.Stdin.Read(seq) // discard escape sequence

		default:
			if b >= 32 && b <= 126 { // printable chars
				password = append(password, b)
				fmt.Fprint(os.Stderr, "*")
			}
		}
	}
}
