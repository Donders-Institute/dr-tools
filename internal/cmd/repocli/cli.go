package repocli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	log "github.com/Donders-Institute/tg-toolset-golang/pkg/logger"
	"github.com/spf13/cobra"

	pb "github.com/schollz/progressbar/v3"
)

// pathFileInfo is an internal data structure containing the absolute `path`
// and the `fs.FileInfo` of a given file.
type pathFileInfo struct {
	path string
	info fs.FileInfo
}

// opInput is a generic input structure for a file operation.
type opInput struct {
	src pathFileInfo
	dst pathFileInfo
}

// Op is the operation options
type Op int

const (
	// Put
	Put Op = iota
	// Get
	Get
	// Move/Rename
	Move
	// Remove
	Remove
	// Copy
	Copy
)

//var dataDir string
var recursive bool
var overwrite bool
var longformat bool
var errfile string

// command to list a file or the content of a directory in the repository.
func lsCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "ls [<repo_file|repo_dir>]",
		Short: "list file or directory in the repository",
		Long: `
The "ls" subcommand is for listing a repository file or the content of a repository directory, with wildcard support.

The optional argument is used to specify the file or directory in the repository to be listed. The argument can be in form of an absolute or relative WebDAV path with the path separator "/", for example, "/dccn/DAC_3010000.01_173/data".

If no argument is provided, it lists the content of the root ("/") WebDAV path.
		`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			p := cwd
			if len(args) == 1 {
				p = getCleanRepoPath(args[0])
			}

			files := make([]fs.FileInfo, 0)

			// check path state
			if f, err := cli.Stat(p); err == nil {
				if !f.IsDir() {
					// path is a file
					files = append(files, f)
				} else {
					// path is a dir, read the entire content of the dir
					if files, err = cli.ReadDir(p); err != nil {
						return err
					}
				}
			} else {
				// check if it is about wildcard listing
				var pat string
				p, pat = path.Split(p)
				if strings.ContainsAny(pat, `*?[`) {
					// wildcard listing, read the entire content of the parent dir
					if entries, err := cli.ReadDir(p); err != nil {
						return err
					} else {
						// filter out entries with name matches the glob
						for _, f := range entries {
							if m, _ := path.Match(pat, f.Name()); m {
								files = append(files, f)
							}
						}
					}
				} else {
					return err
				}
			}

			// listing
			if longformat {
				for _, f := range files {
					fmt.Printf("%11s %12d %s %s\n", f.Mode(), f.Size(), f.ModTime().Format(time.UnixDate), path.Join(p, f.Name()))
				}
			} else {
				isDirMarker := make(map[bool]rune, 2)
				isDirMarker[true] = '/'
				isDirMarker[false] = 0

				for _, f := range files {
					if f.Name() == "" {
						// in case of listing a single file, the `f.Name`` from the `cli.State` is empty.
						// we need to get the filename from the input argument `p`
						fmt.Printf("%s%c\n", path.Base(p), isDirMarker[f.Mode().IsDir()])
						continue
					}
					fmt.Printf("%s%c\n", f.Name(), isDirMarker[f.Mode().IsDir()])
				}
			}
			return nil
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode && len(args) == 0 {
				p := cwd
				if toComplete != "" {
					p = toComplete
				}
				return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}

	cmd.Flags().BoolVarP(&longformat, "long", "l", false, "list files with more detail")

	return cmd
}

func putCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "put <local_file|local_dir> <repo_file|repo_dir>",
		Short: "upload file or directory to the repository",
		Long: `
The "put" subcommand is for uploading a file or a directory from the local filesystem to the repository. It takes two mandatory input arguments for the upload source and destination, respectively.

The first argument specifies the path of a local file/directory as the upload "source".  It can be an absolute or relative path with the os-dependent path separator.

The second argument specifies the WebDAV path of a file/directory in the repository as the upload "destination".  It can be an absolute or relative path with the path separator "/".

When uploading a directory recursively, the tailing "/" on the source path instructs the tool to upload "the content" into the destination. If the tailing "/" is left out, it will upload "the directory by name" in to the destination, resulting in the content being put into a (new) sub-directory in the destination.

For example, 

	$ repocli put /tmp/data /dccn/DAC_3010000.01_173/data

results in the content of /tmp/data being uploaded into a new repository directory /dccn/DAC_3010000.01_173/data/data in the repository; while

	$ repocli put /tmp/data/ /dccn/DAC_3010000.01_173/data

will have the content of /tmp/data uploaded into /dccn/DAC_3010000.01_173/data.

By default, the upload process will skip existing files already in the repository.  One can use the "-f" flag to overwrite existing files.
	`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			// resolve into absolute path at local
			lfpath, err := filepath.Abs(args[0])

			if err != nil {
				return err
			}

			// get local path's FileInfo
			lfinfo, err := os.Stat(lfpath)
			if err != nil {
				return err
			}

			// handle signal for interruption
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			go func() {
				trapCancel(ctx)
				log.Debugf("stopping command: %s\n", cmd.Name())
				cancel()
			}()

			// a file or a directory
			pfinfoLocal := pathFileInfo{
				path: lfpath,
				info: lfinfo,
			}

			p := getCleanRepoPath(args[1])
			f, rerr := cli.Stat(p)

			if lfinfo.IsDir() {

				if rerr == nil && !f.IsDir() {
					return fmt.Errorf("destination not a directory: %s", args[1])
				}

				// the source does not have a tailing path separator.  The whole local directory is
				// created within the specifified directory
				cpath := []rune(args[0])
				if cpath[len(cpath)-1] != os.PathSeparator {
					p = path.Join(p, lfinfo.Name())
				}

				log.Debugf("upload content of %s into %s", pfinfoLocal.path, p)

				// repo pathInfo
				pfinfoRepo := pathFileInfo{
					path: p,
				}

				// create top-level directory in advance
				cli.MkdirAll(pfinfoRepo.path, pfinfoLocal.info.Mode())

				// start progress showing transfer rate in bytes
				pbar := initDynamicMaxProgressbar("uploading...", true)

				// walk through repo directories
				ichan := make(chan opInput)
				go func() {
					walkLocalDirForPut(ctx, pfinfoLocal, pfinfoRepo, ichan, true, pbar)
					pbar.ChangeMax(pbar.GetMax() - 1)
				}()

				// perform data transfer with 4 concurrent workers
				cntOk, cntErr := runOp(ctx, Put, ichan, 4, pbar)

				// log statistics
				if !silent {
					log.Infof("no. succeeded: %d, no. failed: %d", cntOk, cntErr)
				}

				return nil
			} else {

				// path exists in collection, and it is a directory
				// the uploaded file will be put into the directory with the same filename.
				if rerr == nil && f.IsDir() {
					p = path.Join(p, lfinfo.Name())
				}

				// construct filepath at repository
				pfinfoRepo := pathFileInfo{
					path: p,
				}
				return putRepoFile(pfinfoLocal, pfinfoRepo, true)
			}
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode {
				switch len(args) {
				case 0:
					p := lcwd
					if toComplete != "" {
						p = toComplete
					}
					return append([]string{".", ".."}, getContentNamesLocal(p, false)...), cobra.ShellCompDirectiveNoFileComp
				case 1:
					p := cwd
					if toComplete != "" {
						p = toComplete
					}
					return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
				}
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}

	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "overwrite the destination file")
	cmd.Flags().StringVarP(&errfile, "error", "e", "", "save upload errors to the specified `file`")

	return cmd
}

func getCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "get <repo_file|repo_dir> <local_file|local_dir>",
		Short: "download file or directory from the repository",
		Long: `
The "get" subcommand is for downloading a file or a directory from the repository to the local filesystem. It takes two mandatory input arguments for the download source and destination, respectively.

The first argument specifies the WebDAV path of a file/directory in the repository as the download "source". It can be an absolute or relative path with the path separator "/".

The second argument specifies the local filesystem path of a file/directory as the download "destination". It can be an absolute or relative path with the os-dependent path separator.

When downloading a directory recursively, the tailing "/" on the source path instructs the tool to download "the content" into the destination. If the tailing "/" is left out, it will download "the directory by name" in to the destination, resulting in the content being put into a (new) sub-directory in the destination.

For example, 

	$ repocli get /dccn/DAC_3010000.01_173/data /tmp/data

results in the content of /dccn/DAC_3010000.01_173/data being downloaded into a new local directory /tmp/data/data; while

	$ repocli get /dccn/DAC_3010000.01_173/data/ /tmp/data

will have the content of /dccn/DAC_3010000.01_173/data downloaded into /tmp/data.

By default, the download process will skip existing files already in the repository.  One can use the "-f" flag to overwrite existing files.
	`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			p := getCleanRepoPath(args[0])

			f, err := cli.Stat(p)
			if err != nil {
				return err
			}

			// repo pathInfo
			pfinfoRepo := pathFileInfo{
				path: p,
				info: f,
			}

			// get local path's FileInfo
			lp, err := filepath.Abs(args[1])
			if err != nil {
				return err
			}

			// handle signal for interruption
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			go func() {
				trapCancel(ctx)
				log.Debugf("stopping command: %s\n", cmd.Name())
				cancel()
			}()

			lfinfo, lerr := os.Stat(lp)

			// download recursively
			if f.IsDir() {

				// destination path exists but not a directory.
				if lerr == nil && !lfinfo.IsDir() {
					return fmt.Errorf("destination not a directory: %s", args[1])
				}

				// the source does not have a tailing path separator.  The whole local directory is
				// created within the specifified directory
				cpath := []rune(args[0])
				if cpath[len(cpath)-1] != '/' {
					lp = filepath.Join(lp, path.Base(p))
				}

				pfinfoLocal := pathFileInfo{
					path: lp,
				}

				log.Debugf("download content of %s into %s", pfinfoRepo.path, pfinfoLocal.path)

				if err := os.MkdirAll(lp, pfinfoRepo.info.Mode()); err != nil {
					return err
				}

				// progress bar showing transfer rate in bytes
				pbar := initDynamicMaxProgressbar("downloading...", true)

				// walk through repo directories
				ichan := make(chan opInput)
				go func() {
					walkRepoDirForGet(ctx, pfinfoRepo, pfinfoLocal, ichan, true, pbar)
					pbar.ChangeMax(pbar.GetMax() - 1)
				}()

				// perform data transfer with 4 concurrent workers
				cntOk, cntErr := runOp(ctx, Get, ichan, 4, pbar)

				// log statistics
				if !silent {
					log.Infof("no. succeeded: %d, no. failed: %d", cntOk, cntErr)
				}

				return nil

			} else {

				if lerr == nil && lfinfo.IsDir() {
					lp = filepath.Join(lp, path.Base(p))
				}

				pfinfoLocal := pathFileInfo{
					path: lp,
				}

				// download single file
				return getRepoFile(pfinfoRepo, pfinfoLocal, !silent)
			}

		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode {
				switch len(args) {
				case 0:
					p := cwd
					if toComplete != "" {
						p = toComplete
					}
					return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
				case 1:
					p := lcwd
					if toComplete != "" {
						p = toComplete
					}
					return append([]string{".", ".."}, getContentNamesLocal(p, false)...), cobra.ShellCompDirectiveNoFileComp
				}
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}

	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "overwrite the destination file")
	cmd.Flags().StringVarP(&errfile, "error", "e", "", "save download errors to the specified `file`")

	return cmd
}

func cpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cp <repo_file|repo_dir> <repo_file|repo_dir>",
		Short: "copy file or directory in the repository",
		Long: `
The "cp" subcommand is for copying a file or a directory in the repository. It takes two mandatory input arguments.

The first argument specifies an existing WebDAV path of a file/directory in the repository as the "source". It can be an absolute or relative path with the path separator "/".

The second argument specifies another WebDAV path of a file/directory in the repository as the "destination". It can be an absolute or relative path with the path separator "/".

When copying a directory recursively, the tailing "/" on the source path instructs the tool to copy "the content" into the destination. If the tailing "/" is left out, it will copy "the directory by name" in to the destination, resulting in the content being copied into a (new) sub-directory in the destination.

For example, 

	$ repocli cp /dccn/DAC_3010000.01_173/data /dccn/DAC_3010000.01_173/data.new

results in the content of /dccn/DAC_3010000.01_173/data being copied into a new repository directory /dccn/DAC_3010000.01_173/data.new/data; while

	$ repocli cp /dccn/DAC_3010000.01_173/data/ /dccn/DAC_3010000.01_173/data.new

will have the content of /dccn/DAC_3010000.01_173/data copied into /dccn/DAC_3010000.01_173/data.new.

By default, the copy process will skip existing files at the destination.  One can use the "-f" flag to overwrite existing files.
	`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			src := getCleanRepoPath(args[0])
			dst := getCleanRepoPath(args[1])

			// source must exists
			fsrc, err := cli.Stat(src)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			go func() {
				trapCancel(ctx)
				log.Debugf("stopping command: %s\n", cmd.Name())
				cancel()
			}()

			fdst, derr := cli.Stat(dst)

			if fsrc.IsDir() {

				// destination path exists but not a directory.
				if derr == nil && !fdst.IsDir() {
					return fmt.Errorf("destination not a directory: %s", args[1])
				}

				// the source does not have a tailing path separator.  The whole local directory is
				// created within the specifified directory
				cpath := []rune(args[0])
				if cpath[len(cpath)-1] != '/' {
					dst = path.Join(dst, path.Base(src))
				}

				pfinfoSrc := pathFileInfo{
					path: src,
					info: fsrc,
				}

				pfinfoDst := pathFileInfo{
					path: dst,
				}

				log.Debugf("copying %s to %s", pfinfoSrc.path, pfinfoDst.path)

				if err := cli.MkdirAll(dst, pfinfoSrc.info.Mode()); err != nil {
					return err
				}

				// start progress with copying rate in number of copied files
				pbar := initDynamicMaxProgressbar("copying...", false)

				// run with 4 concurrent workers
				cntOk, cntErr, err := copyOrMoveRepoDir(ctx, Copy, pfinfoSrc, pfinfoDst, pbar)

				pbar.ChangeMax(pbar.GetMax() - 1)

				// log statistics
				if !silent {
					log.Infof("no. succeeded: %d, no. failed: %d", cntOk, cntErr)
				}

				if err != nil {
					return err
				}

				return nil
			} else {
				if derr == nil && fdst.IsDir() {
					dst = path.Join(dst, path.Base(src))
				}
				log.Debugf("copying %s to %s", src, dst)
				return cli.Copy(src, dst, overwrite)
			}
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode && len(args) < 2 {
				p := cwd
				if toComplete != "" {
					p = toComplete
				}
				return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "overwrite the destination file")
	return cmd
}

func mvCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "mv <repo_file|repo_dir> <repo_file|repo_dir>",
		Short: "move file or directory in the repository",
		Long: `
The "mv" subcommand is for moving a file or a directory in the repository. It takes two mandatory input arguments.

The first argument specifies an existing WebDAV path of a file/directory in the repository as the "source". It can be an absolute or relative path with the path separator "/".

The second argument specifies another WebDAV path of a file/directory in the repository as the "destination". It can be an absolute or relative path with the path separator "/".

When moving a directory recursively, the tailing "/" on the source path instructs the tool to move "the content" into the destination. If the tailing "/" is left out, it will move "the directory by name" in to the destination, resulting in the content being moved into a (new) sub-directory in the destination.

For example,

	$ repocli mv /dccn/DAC_3010000.01_173/data /dccn/DAC_3010000.01_173/data.new

results in the content of /dccn/DAC_3010000.01_173/data being moved into a new repository directory /dccn/DAC_3010000.01_173/data.new/data; while

	$ repocli mv /dccn/DAC_3010000.01_173/data/ /dccn/DAC_3010000.01_173/data.new

will have the content of /dccn/DAC_3010000.01_173/data moved into /dccn/DAC_3010000.01_173/data.new.

By default, the move process will skip existing files at the destination.  One can use the "-f" flag to overwrite existing files.

Files not successfully moved over will be kept at the source.
	`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {

			src := getCleanRepoPath(args[0])
			dst := getCleanRepoPath(args[1])

			// source must exists
			fsrc, err := cli.Stat(src)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			go func() {
				trapCancel(ctx)
				log.Debugf("stopping command: %s\n", cmd.Name())
				cancel()
			}()

			fdst, derr := cli.Stat(dst)

			if fsrc.IsDir() {

				// destination path exists but not a directory.
				if derr == nil && !fdst.IsDir() {
					return fmt.Errorf("destination not a directory: %s", args[1])
				}

				// the source does not have a tailing path separator.  The whole local directory is
				// created within the specifified directory
				cpath := []rune(args[0])
				if cpath[len(cpath)-1] != '/' {
					dst = path.Join(dst, path.Base(src))
				}

				pfinfoSrc := pathFileInfo{
					path: src,
					info: fsrc,
				}

				pfinfoDst := pathFileInfo{
					path: dst,
				}

				log.Debugf("renaming %s to %s", pfinfoSrc.path, pfinfoDst.path)

				if err := cli.MkdirAll(dst, pfinfoSrc.info.Mode()); err != nil {
					return err
				}

				// start progress in moving rate in number of moved files
				pbar := initDynamicMaxProgressbar("moving...", true)

				// perform data transfer with 4 concurrent workers
				cntOk, cntErr, err := copyOrMoveRepoDir(ctx, Move, pfinfoSrc, pfinfoDst, pbar)

				pbar.ChangeMax(pbar.GetMax() - 1)

				// log statistics
				if !silent {
					log.Infof("no. succeeded: %d, no. failed: %d", cntOk, cntErr)
				}

				if err != nil {
					return err
				}

				return nil
			} else {
				if derr == nil && fdst.IsDir() {
					dst = path.Join(dst, path.Base(src))
				}
				log.Debugf("renaming %s to %s", src, dst)
				return cli.Rename(src, dst, overwrite)
			}
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode && len(args) < 2 {
				p := cwd
				if toComplete != "" {
					p = toComplete
				}
				return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "f", false, "overwrite the destination file")
	return cmd
}

func rmCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "rm <file_repo|directory_repo>",
		Short: "remove file or directory from the repository",
		Long: `
The "rm" subcommand is for removing a file or a directory in the repository.

The mandatory argument is used to specify the file or directory in the repository to be removed.  The argument can be in form of an absolute or relative WebDAV path with the path separator "/", for example, "/dccn/DAC_3010000.01_173/data".

When removing a directory containing files or sub-directories, the flag "-r" should be applied to do the removal recursively.
		`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			rp := getCleanRepoPath(args[0])

			f, err := cli.Stat(rp)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			go func() {
				trapCancel(ctx)
				log.Debugf("stopping command: %s\n", cmd.Name())
				cancel()
			}()

			if f.IsDir() {

				// start progress with removing rate in number of files
				pbar := initDynamicMaxProgressbar("removing...", false)

				// perform data transfer with 4 concurrent workers
				cntOk, cntErr, err := rmRepoDir(ctx, rp, recursive, pbar)

				pbar.ChangeMax(pbar.GetMax() - 1)

				// log statistics
				if !silent {
					log.Infof("no. succeeded: %d, no. failed: %d", cntOk, cntErr)
				}

				if err != nil {
					return err
				}

				return nil
			} else {
				return cli.Remove(rp)
			}
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			// get list of content in this directory
			if shellMode && len(args) == 0 {
				p := cwd
				if toComplete != "" {
					p = toComplete
				}
				return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveError
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove directory recursively")
	return cmd
}

var mkdirCmd = &cobra.Command{
	Use:   "mkdir <repo_dir>",
	Short: "create new directory in the repository",
	Long: `
The "mkdir" subcommand is for creating a new directory in the repository.

The mandatory argument is used to specify the new directory in the repository to be created. The argument can be in form of an absolute or relative WebDAV path with the path separator "/", for example, "/dccn/DAC_3010000.01_173/data".

During the creation, any missing parents directories are also created automatically (only if the user is authorized).
`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cli.MkdirAll(getCleanRepoPath(args[0]), 0664)
	},
	ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// get list of content in this directory
		if shellMode && len(args) == 0 {
			p := cwd
			if toComplete != "" {
				p = toComplete
			}
			return append([]string{".", ".."}, getContentNamesRepo(p, false)...), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveError
	},
}

// getCleanRepoPath resolves provided path into a clean absolute path taking into account
// the `cwd`.
func getCleanRepoPath(p string) string {
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	return path.Join(cwd, p)
}

// runOp performs file operation `Op` with input data provided through the
// channel `ichan`.
func runOp(ctx context.Context, op Op, ichan chan opInput, nworkers int, pbar *pb.ProgressBar) (cntOk, cntErr int) {

	// error log writer
	errWriter := os.Stderr
	if errfile != "" {
		f, err := os.OpenFile(errfile, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Errorf("cannot open file %s for error log: ", errfile, err)
		}
		defer f.Close()
		errWriter = f
	}

	// initalize concurrent workers
	var wg sync.WaitGroup
	for i := 0; i < nworkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
		loop:
			for {
				select {
				case <-ctx.Done():
					log.Debugf("stopping runOp for %v\n", op)
					break loop
				case inputs, ok := <-ichan:

					if !ok {
						break loop
					}

					var err error
					pinc := int64(1) // progress increment
					switch op {
					case Put:
						err = putRepoFile(inputs.src, inputs.dst, false)
						pinc = inputs.src.info.Size()
					case Get:
						err = getRepoFile(inputs.src, inputs.dst, false)
						pinc = inputs.src.info.Size()
					case Move:
						err = cli.Rename(inputs.src.path, inputs.dst.path, overwrite)
					case Remove:
						err = cli.Remove(inputs.src.path)
					case Copy:
						cli.Copy(inputs.src.path, inputs.dst.path, overwrite)
					default:
						// do nothing
						err = fmt.Errorf("unknown operation: %d", op)
					}
					if err != nil {
						fmt.Fprintf(errWriter, "%s error:%s\n", inputs.src.path, err.Error())
						cntErr += 1
					} else {
						cntOk += 1
					}
					pbar.Add64(pinc)
				}
			}
		}()
	}

	// wait for workers to be released
	wg.Wait()

	return
}

// walkLocalDirForPut walks through a local directory and creates inputs for putting files from local to repo.
func walkLocalDirForPut(ctx context.Context, pfinfoLocal, pfinfoRepo pathFileInfo, ichan chan opInput, closeChanOnComplete bool, pbar *pb.ProgressBar) {
	// read the entire content of the dir
	files, err := ioutil.ReadDir(pfinfoLocal.path)
	if err != nil {
		return
	}

	pbar.ChangeMax64(pbar.GetMax64() + countSize(files))

loop:
	for _, finfo := range files {
		select {
		case <-ctx.Done():
			log.Debugf("stopping walkRepoDirForPut ...\n")
			break loop
		default:
			p := path.Join(pfinfoLocal.path, finfo.Name())

			// local dir pathInfo
			_pfinfoLocal := pathFileInfo{
				path: p,
				info: finfo,
			}
			// repo dir pathInfo
			_pfinfoRepo := pathFileInfo{
				path: path.Join(pfinfoRepo.path, finfo.Name()),
			}

			if finfo.IsDir() {
				// create sub directory in advance
				if err := cli.Mkdir(_pfinfoRepo.path, _pfinfoLocal.info.Mode()); err != nil {
					log.Errorf("cannot create repo dir %s: %s", _pfinfoRepo.path, err)
					continue
				}
				// walk into sub directory without closing the channel
				walkLocalDirForPut(ctx, _pfinfoLocal, _pfinfoRepo, ichan, false, pbar)
			} else {
				ichan <- opInput{
					src: _pfinfoLocal,
					dst: _pfinfoRepo,
				}
			}
		}
	}

	if closeChanOnComplete {
		close(ichan)
	}
}

// walkRepoDirForGet walks through a repo directory and creates inputs for getting files from repo to local.
func walkRepoDirForGet(ctx context.Context, pfinfoRepo, pfinfoLocal pathFileInfo, ichan chan opInput, closeChanOnComplete bool, pbar *pb.ProgressBar) {

	// read the entire content of the dir
	files, err := cli.ReadDir(pfinfoRepo.path)
	if err != nil {
		return
	}

	// push number of total files in this directory for updating progress bar
	pbar.ChangeMax64(pbar.GetMax64() + countSize(files))

	// loop over content
loop:
	for _, finfo := range files {
		select {
		case <-ctx.Done():
			log.Debugf("stopping walkRepoDirForGet ...\n")
			break loop
		default:
			p := path.Join(pfinfoRepo.path, finfo.Name())

			// repo dir pathInfo
			_pfinfoRepo := pathFileInfo{
				path: p,
				info: finfo,
			}
			// local dir pathInfo
			_pfinfoLocal := pathFileInfo{
				path: filepath.Join(pfinfoLocal.path, finfo.Name()),
			}

			if finfo.IsDir() {
				// create sub directory in advance
				if err := os.MkdirAll(_pfinfoLocal.path, _pfinfoRepo.info.Mode()); err != nil {
					log.Errorf("cannot create local dir %s: %s", _pfinfoLocal.path, err)
					continue
				}
				// walk into sub directory without closing the channel
				walkRepoDirForGet(ctx, _pfinfoRepo, _pfinfoLocal, ichan, false, pbar)
			} else {
				ichan <- opInput{
					src: _pfinfoRepo,
					dst: _pfinfoLocal,
				}
			}
		}
	}

	if closeChanOnComplete {
		close(ichan)
	}
}

// is404NotFountError checkes if the `err.Error()` contains a string `404 Not Found`.
func is404NotFountError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "404 Not Found")
}

// putRepoFile uploads a single local file to the repository.
func putRepoFile(pfinfoLocal, pfinfoRepo pathFileInfo, showProgress bool) error {

	if !overwrite {
		// don't want existing files to be overwritten
		if _, err := cli.Stat(pfinfoRepo.path); !is404NotFountError(err) {
			log.Debugf("skip existing file %s\n", pfinfoRepo.path)
			return nil
		}
	}

	// open pathLocal
	reader, err := os.Open(pfinfoLocal.path)
	if err != nil {
		return fmt.Errorf("cannot open local file: %s", err)
	}
	defer reader.Close()

	// progress bar
	barDesc := prettifyProgressbarDesc(pfinfoLocal.info.Name())
	bar := pb.DefaultBytesSilent(pfinfoLocal.info.Size(), barDesc)
	if showProgress {
		bar = pb.DefaultBytes(pfinfoLocal.info.Size(), barDesc)
	}

	// read pathRepo and write to pathLocal, the mode is not actually useful (!?)
	err = cli.WriteStream(pfinfoRepo.path, reader, pfinfoLocal.info.Mode())
	if err != nil {
		return fmt.Errorf("cannot write %s to the repository: %s", pfinfoRepo.path, err)
	}

	// file size check after upload
	f, err := cli.Stat(pfinfoRepo.path)
	if err != nil {
		return fmt.Errorf("cannot stat %s at the repository: %s", pfinfoRepo.path, err)
	}

	if f.Size() != pfinfoLocal.info.Size() {
		return fmt.Errorf("file size %s mis-match: %d != %d", pfinfoRepo.path, f.Size(), pfinfoLocal.info.Size())
	}

	// TODO: this jumps from 0% to 100% ... not ideal but there is no way with to get upload progression with the webdav client library
	bar.Add64(f.Size())

	return nil
}

// getRepoFile downloads a single file from the repository to a local file.
func getRepoFile(pfinfoRepo, pfinfoLocal pathFileInfo, showProgress bool) error {

	if !overwrite {
		// don't want existing files to be overwritten.
		if _, err := os.Stat(pfinfoLocal.path); !errors.Is(err, os.ErrNotExist) {
			log.Debugf("skip existing file %s\n", pfinfoLocal.path)
			return nil
		}
	}

	// open pathLocal
	fileLocal, err := os.OpenFile(pfinfoLocal.path, os.O_WRONLY|os.O_CREATE, pfinfoRepo.info.Mode())
	if err != nil {
		return fmt.Errorf("cannot create/write local file: %s", err)
	}
	defer fileLocal.Close()

	// progress bar
	barDesc := prettifyProgressbarDesc(path.Base(pfinfoRepo.path))
	bar := pb.DefaultBytesSilent(pfinfoRepo.info.Size(), barDesc)
	if showProgress {
		bar = pb.DefaultBytes(pfinfoRepo.info.Size(), barDesc)
	}

	// multiwriter: destination local file, and progress bar
	writer := io.MultiWriter(fileLocal, bar)

	// read pathRepo and write to pathLocal
	reader, err := cli.ReadStream(pfinfoRepo.path)
	if err != nil {
		return fmt.Errorf("cannot open file in repository: %s", err)
	}
	defer reader.Close()

	buffer := make([]byte, 4*1024*1024) // 4MiB buffer

	for {
		// read content to buffer
		rlen, rerr := reader.Read(buffer)
		if rerr != nil && rerr != io.EOF {
			return fmt.Errorf("failure reading data from %s: %s", pfinfoRepo.path, rerr)
		}
		wlen, werr := writer.Write(buffer[:rlen])
		if werr != nil || rlen != wlen {
			return fmt.Errorf("failure writing data to %s: %s", pfinfoLocal.path, err)
		}

		if rerr == io.EOF {
			break
		}
	}

	return nil
}

// simple webdav client wrapper to switch between Copy and Rename.
func cliCopyOrRename(op Op, src, dst string) error {
	if op == Move {
		if err := cli.Rename(src, dst, overwrite); err != nil {
			log.Errorf("cannot rename repo file %s: %s", src, err)
			return err
		}
	} else {
		if err := cli.Copy(src, dst, overwrite); err != nil {
			log.Errorf("cannot copy repo file %s: %s", src, err)
			return err
		}
	}
	return nil
}

// copyOrMoveRepoDir moves directory from `src` to `dst` recursively.
func copyOrMoveRepoDir(ctx context.Context, op Op, src, dst pathFileInfo, pbar *pb.ProgressBar) (cntOk, cntErr int, err error) {

	// read the entire content of the source directory
	files, err := cli.ReadDir(src.path)
	if err != nil {
		return
	}

	pbar.ChangeMax64(pbar.GetMax64() + countFiles(files))

	// make attempt to create all parent directories of the destination.
	cli.MkdirAll(dst.path, src.info.Mode())

	// channel for getting files concurrently.
	fchan := make(chan pathFileInfo)

	// initalize concurrent workers
	var wg sync.WaitGroup
	nworkers := 4
	// counter for file deletion error by worker
	cntErrFiles := make([]int, nworkers)
	cntOkFiles := make([]int, nworkers)
	for i := 0; i < nworkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
		loop:
			for finfo := range fchan {
				select {
				case <-ctx.Done():
					log.Debugf("stopping %v ...\n", op)
					break loop
				default:
					if err := cliCopyOrRename(op, finfo.path, path.Join(dst.path, finfo.info.Name())); err != nil {
						cntErrFiles[id]++
					} else {
						cntOkFiles[id]++
					}
					pbar.Add(1)
				}
			}
		}(i)
	}

	// loop over content
loop:
	for _, finfo := range files {
		select {
		case <-ctx.Done():
			log.Debugf("stopping dir walk for %v ...\n", op)
			break loop
		default:
			psrc := path.Join(src.path, finfo.Name())
			pdst := path.Join(dst.path, path.Base(finfo.Name()))
			if finfo.IsDir() {
				// repo dir pathInfo
				_pfinfoSrc := pathFileInfo{
					path: psrc,
					info: finfo,
				}

				_pfinfoDst := pathFileInfo{
					path: pdst,
				}

				_cntOk, _cntErr, _ := copyOrMoveRepoDir(ctx, op, _pfinfoSrc, _pfinfoDst, pbar)
				cntErr += _cntErr
				cntOk += _cntOk
			} else {
				fchan <- pathFileInfo{
					path: psrc,
					info: finfo,
				}
			}
		}
	}

	// close fchan to release workers
	close(fchan)

	// wait for workers to be released
	wg.Wait()

	// update overall deletion errors with error counts from workers
	for i := 0; i < nworkers; i++ {
		cntErr += cntErrFiles[i]
		cntOk += cntOkFiles[i]

	}

	// remove the moved directory only if there is no error
	if op == Move && cntErr == 0 {
		err = cli.Remove(src.path)
	}

	return
}

// rmRepoDir removes the directory `path` from the repository recursively.
func rmRepoDir(ctx context.Context, repoPath string, recursive bool, pbar *pb.ProgressBar) (cntOk, cntErr int, err error) {

	// path on repo should be specified in absolute path form
	if !path.IsAbs(repoPath) {
		err = fmt.Errorf("not an absolute path: %s", repoPath)
		return
	}

	// read the entire content of the dir
	files, err := cli.ReadDir(repoPath)
	if err != nil {
		return
	}

	// directory is not empty, not in recursive mode
	if len(files) > 0 && !recursive {
		err = fmt.Errorf("directory not empty: %s", repoPath)
		return
	}

	pbar.ChangeMax64(pbar.GetMax64() + countFiles(files))

	// channel for deleting files concurrently.
	fchan := make(chan string)

	// initalize concurrent workers
	var wg sync.WaitGroup
	nworkers := 4
	// counter for file deletion error by worker
	cntOkFiles := make([]int, nworkers)
	cntErrFiles := make([]int, nworkers)
	for i := 0; i < nworkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
		loop:
			for f := range fchan {
				select {
				case <-ctx.Done():
					log.Debugf("stopping remove ...\n")
					break loop
				default:
					err := cli.Remove(f)
					if err != nil {
						log.Errorf("cannot remove repo file %s: %s", f, err)
						cntErrFiles[id]++
					} else {
						cntOkFiles[id]++
					}
					pbar.Add(1)
				}
			}
		}(i)
	}

	// loop over content
loop:
	for _, f := range files {
		select {
		case <-ctx.Done():
			log.Debugf("stopping dir walk for remove ...\n")
			break loop
		default:
			p := path.Join(repoPath, f.Name())
			if f.IsDir() {
				_cntOK, _cntErr, _ := rmRepoDir(ctx, p, recursive, pbar)
				cntOk += _cntOK
				cntErr += _cntErr
			} else {
				fchan <- p
			}
		}
	}

	// close fchan to release workers
	close(fchan)

	// wait for workers to be released
	wg.Wait()

	// update overall deletion errors with error counts from workers
	for i := 0; i < nworkers; i++ {
		cntOk += cntOkFiles[i]
		cntErr += cntErrFiles[i]
	}

	// remove the directory itself
	err = cli.Remove(repoPath)
	return
}

// initDynamicMaxProgressbar initiates a new progress bar with a given description.
//
// This function assumes the caller is responsible for updating the bar's max steps
// dynamically, and therefore the initial max is set to `1`. Caller should also reduce
// the final max by `1` due to this artificial initial max, for example:
//
//     bar := initDynamicMaxProgressbar()
//     bar.ChangeMax(bar.GetMax() - 1)
//
func initDynamicMaxProgressbar(desc string, showBytes bool) *pb.ProgressBar {
	if silent {
		if showBytes {
			return pb.DefaultBytesSilent(1, desc)
		}
		return pb.DefaultSilent(1, desc)
	}

	bar := pb.NewOptions64(
		1,
		pb.OptionSetDescription(fmt.Sprintf("%-20s", desc)),
		pb.OptionSetWriter(os.Stderr),
		pb.OptionShowBytes(showBytes),
		pb.OptionSetWidth(10),
		pb.OptionThrottle(65*time.Millisecond),
		pb.OptionShowCount(),
		pb.OptionOnCompletion(func() {
			fmt.Printf("\n")
		}),
		pb.OptionSpinnerType(14),
		pb.OptionFullWidth(),
		pb.OptionSetPredictTime(true),
	)

	bar.RenderBlank()
	return bar
}

// prettifyProgressbarDesc returns a shortened description string up to 15 UTF-8 characters.
func prettifyProgressbarDesc(origin string) string {

	maxLen := 33

	if utf8.RuneCountInString(origin) <= maxLen {
		return fmt.Sprintf("%-33s", origin)
	}

	chars := []rune(origin)

	return fmt.Sprintf("%-15s...%-15s", string(chars[:15]), string(chars[len(origin)-15:]))
}

// countFiles returns number of non-directory files in the given slice of `paths`.
func countFiles(paths []fs.FileInfo) (count int64) {
	for _, p := range paths {
		if !p.IsDir() {
			count += 1
		}
	}
	return
}

// countSize returns total size of non-directory files in the given slice of `paths`.
func countSize(paths []fs.FileInfo) (size int64) {
	for _, p := range paths {
		if !p.IsDir() {
			size += p.Size()
		}
	}
	return
}
