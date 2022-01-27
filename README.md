# Tools for managing data in the Donders Repository

* [repoadm](cmd/repoadm): administrator's CLI for managing the Donders Repository collections, using the iROD's iCommands.
* [repocli](cmd/repocli): cross-platform user CLI for managing data in the Donders Repository, using the WebDAV interface.

## Build

To build the CLIs, simply run:

```bash
make
```

After the build, the executable binaries are located in `${GOPATH}/bin` directory.  If `${GOPATH}` is not set in the environment, default is `${HOME}/go`.

## Release

The [Makefile](Makefile) has a target to build a GitHub release with an RPM package as the release asset.  For making a GitHub release `X.Y.Z`, one does:

```bash
VERSION=X.Y.Z make github-release
```

The [GitHub personal access token](https://help.github.com/en/github/authenticating-to-github/creating-a-personal-access-token-for-the-command-line) is required for interacting with the GitHub APIs.

## Run

All CLI commands have a `-h` option to print out a brief usage of the command.
