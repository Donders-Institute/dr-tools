ifndef GOPATH
	GOPATH := $(HOME)/go
endif

ifndef GOOS
	GOOS := linux
endif

ifndef GO111MODULE
	GO111MODULE := on
endif

VERSION ?= "master"

.PHONY: build

all: build

build:
	GOPATH=$(GOPATH) GOOS=$(GOOS) GO111MODULE=$(GO111MODULE) go install github.com/Donders-Institute/dr-tools/...

build_repocli:
	GOPATH=$(GOPATH) GOOS=linux GOARCH=amd64 GO111MODULE=$(GO111MODULE) go build -o $(GOPATH)/bin/repocli cmd/repocli/main.go

build_repocli_macosx:
	GOPATH=$(GOPATH) GOOS=darwin GOARCH=amd64 GO111MODULE=$(GO111MODULE) go build -o $(GOPATH)/bin/repocli.darwin cmd/repocli/main.go

build_repocli_windows:
	GOPATH=$(GOPATH) GOOS=windows GOARCH=amd64 GO111MODULE=$(GO111MODULE) go build -o $(GOPATH)/bin/repocli.exe cmd/repocli/main.go

test:
	@GOPATH=$(GOPATH) GOOS=$(GOOS) GO111MODULE=$(GO111MODULE) go test -v github.com/Donders-Institute/dr-tools/...

release:
	VERSION=$(VERSION) rpmbuild --undefine=_disable_source_fetch -bb build/rpm/centos7.spec

github-release:
	scripts/gh-release.sh $(VERSION) false

clean:
	@rm -rf $(GOPATH)/bin/repo*
	@rm -rf $(GOPATH)/pkg/$(GOOS)*
