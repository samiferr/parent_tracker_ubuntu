# Parent Tracker — build, test and package. Needs only Go (see go.mod); the
# dashboard has no build step (React 18 and Lumen are vendored, unmodified).

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
ifeq ($(strip $(VERSION)),)
VERSION := 0.1.0-dev
endif
GOARCH  ?= $(shell go env GOARCH)
LDFLAGS := -s -w -X github.com/samiferr/parent_tracker_ubuntu/internal/version.Version=$(VERSION)
BINS    := parent-trackerd parent-tracker parent-tracker-agent

.PHONY: all build test vet fmt check tokens notices deb deb-all clean demo

all: build

build:
	@mkdir -p bin
	@for b in $(BINS); do \
	  CGO_ENABLED=0 GOARCH=$(GOARCH) go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$$b ./cmd/$$b || exit 1; \
	done
	@ls -l bin

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w cmd internal tools

# check is what CI runs: formatting, vet, tokens.css up to date, tests.
check:
	@test -z "$$(gofmt -l cmd internal tools)" || { gofmt -l cmd internal tools; echo "run: make fmt"; exit 1; }
	go vet ./...
	go run ./tools/lumen-tokens -check
	packaging/gen-notices.sh --check
	go test -race -count=1 ./...

# Regenerate tokens.css from the vendored (unmodified) Lumen tokens.json.
tokens:
	go run ./tools/lumen-tokens

# Regenerate the third-party licence notices (also the package's copyright file).
notices:
	packaging/gen-notices.sh

# Try the dashboard with made-up data, nothing real is recorded.
demo: build
	./bin/parent-tracker serve --demo

# .deb for the host architecture; deb-all builds amd64 and arm64. Needs dpkg-deb.
# The version is made Debian-valid by packaging/build-deb.sh.
deb:
	@mkdir -p dist/build/$(GOARCH)
	@for b in $(BINS); do \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags '$(LDFLAGS)' -o dist/build/$(GOARCH)/$$b ./cmd/$$b || exit 1; \
	done
	packaging/build-deb.sh $(VERSION) $(GOARCH) dist/build/$(GOARCH)

deb-all:
	$(MAKE) deb GOARCH=amd64
	$(MAKE) deb GOARCH=arm64

clean:
	rm -rf bin dist
