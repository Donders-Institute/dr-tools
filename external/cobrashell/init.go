// This code is modified on a very nice project called cobra-shell done by Brian Strauch.
//
// The project github page: https://github.com/brianstrauch/cobra-shell
//
package cobrashell

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/c-bata/go-prompt"
	"github.com/google/shlex"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type cobraShell struct {
	// root cobra.Command to which the shell command will be attached to
	root *cobra.Command
	// newRoot is a function to generate a new root object for executing command and resolving completion suggestions
	// It is needed for workarounding the flag leakage of cobra, which is not designed for interactive shell.
	// see discussions in https://github.com/spf13/cobra/issues/279
	newRoot func() *cobra.Command
	// cached completion suggestion
	cache map[string][]prompt.Suggest
	stdin *term.State
}

var exitCmd = &cobra.Command{
	Use:   "exit",
	Short: "exit the interactive shell",
	Run: func(*cobra.Command, []string) {
		// TODO: Exit cleanly without help from the os package
		os.Exit(0)
	},
}

// New creates a Cobra CLI command named "shell" which runs an interactive shell prompt for the root command.
func New(root *cobra.Command, newRoot func() *cobra.Command, opts ...prompt.Option) *cobra.Command {
	shell := &cobraShell{
		root:    root,
		newRoot: newRoot,
		cache:   make(map[string][]prompt.Suggest),
	}

	prefix := fmt.Sprintf("> %s ", root.Name())
	opts = append(opts, prompt.OptionPrefix(prefix), prompt.OptionShowCompletionAtStart())

	return &cobra.Command{
		Use:   "shell",
		Short: "start an interactive shell",
		Run: func(cmd *cobra.Command, _ []string) {
			shell.editCommandTree(cmd)
			shell.saveStdin()

			prompt := prompt.New(shell.executor, shell.completer, opts...)
			prompt.Run()

			shell.restoreStdin()
		},
	}
}

func (s *cobraShell) editCommandTree(shell *cobra.Command) {
	s.root.RemoveCommand(shell)

	// Hide the "completion" command
	if cmd, _, err := s.root.Find([]string{"completion"}); err == nil {
		// TODO: Remove this command
		cmd.Hidden = true
	}

	s.root.AddCommand(exitCmd)
}

func (s *cobraShell) saveStdin() {
	state, err := term.GetState(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	s.stdin = state
}

func (s *cobraShell) executor(line string) {

	// Allow command to read from stdin
	s.restoreStdin()

	args, err := shlex.Split(line)
	if err != nil {
		args = strings.Fields(line)
	}

	// create a new root Command to execute the input instead of using s.root
	cmd := s.newRoot()
	cmd.AddCommand(exitCmd)

	_ = execute(cmd, args)

	rootKey := "__complete "
	s.cache = map[string][]prompt.Suggest{rootKey: s.cache[rootKey]}
}

func (s *cobraShell) restoreStdin() {
	if s.stdin != nil {
		_ = term.Restore(int(os.Stdin.Fd()), s.stdin)
	}
}

func (s *cobraShell) completer(d prompt.Document) []prompt.Suggest {
	args, err := buildCompletionArgs(d.CurrentLine())
	if err != nil {
		return nil
	}

	suggestionPathPrefix := ""
	switch {
	case isLocalDir(args[len(args)-1]):
		pdir := filepath.Dir(args[len(args)-1])
		if pdir == string(os.PathSeparator) {
			pdir = ""
		}
		suggestionPathPrefix = fmt.Sprintf("%s%c", pdir, os.PathSeparator)
		args[len(args)-1] = suggestionPathPrefix
	case isUnixDir(args[len(args)-1]):
		pdir := path.Dir(args[len(args)-1])
		if pdir == "/" {
			pdir = ""
		}
		suggestionPathPrefix = fmt.Sprintf("%s%c", pdir, '/')
		args[len(args)-1] = suggestionPathPrefix
	case !isFlag(args[len(args)-1]):
		args[len(args)-1] = ""
	}

	key := strings.Join(args, " ")

	suggestions, ok := s.cache[key]
	if !ok {
		// create a new root `cobra.Command` to resolve suggestions
		// it is somehow needed to prevent messing up the output of go-prompt??
		// TODO: find a better way of doing it, because it will keep creating new root `cobra.Command`
		//       for every tab pressed.
		cmd := s.newRoot()
		cmd.AddCommand(exitCmd)
		out, err := readCommandOutput(cmd, args)

		if err != nil {
			return nil
		}
		suggestions = parseSuggestions(out, suggestionPathPrefix)
		s.cache[key] = suggestions
	}

	return prompt.FilterHasPrefix(suggestions, d.GetWordBeforeCursor(), true)
}

func buildCompletionArgs(input string) ([]string, error) {
	args, err := shlex.Split(input)

	args = append([]string{"__complete"}, args...)
	if input == "" || input[len(input)-1] == ' ' {
		args = append(args, "")
	}

	return args, err
}

func readCommandOutput(cmd *cobra.Command, args []string) (string, error) {
	buf := new(bytes.Buffer)

	stdout := cmd.OutOrStdout()
	stderr := os.Stderr

	cmd.SetOut(buf)
	_, os.Stderr, _ = os.Pipe()

	err := execute(cmd, args)

	cmd.SetOut(stdout)
	os.Stderr = stderr

	return buf.String(), err
}

func execute(cmd *cobra.Command, args []string) error {
	cmd.SetArgs(args)
	return cmd.Execute()
}

func parseSuggestions(out, suggestionTextPrefix string) []prompt.Suggest {
	var suggestions []prompt.Suggest

	x := strings.Split(out, "\n")
	if len(x) < 2 {
		return nil
	}

	for _, line := range x[:len(x)-2] {
		if line != "" {
			x := strings.SplitN(line, "\t", 2)

			var description string
			if len(x) > 1 {
				description = x[1]
			}

			suggestions = append(suggestions, prompt.Suggest{
				Text:        fmt.Sprintf("%s%s", suggestionTextPrefix, escapeSpecialCharacters(x[0])),
				Description: description,
			})
		}
	}

	return suggestions
}

func escapeSpecialCharacters(val string) string {
	for _, c := range []string{"\\", "\"", "$", "`", "!"} {
		val = strings.ReplaceAll(val, c, "\\"+c)
	}

	if strings.ContainsAny(val, " #&*;<>?[]|~") {
		val = fmt.Sprintf(`"%s"`, val)
	}

	return val
}

func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-")
}

func isLocalDir(arg string) bool {
	return strings.ContainsRune(arg, os.PathSeparator)
}

func isUnixDir(arg string) bool {
	return strings.ContainsRune(arg, '/')
}
