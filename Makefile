# BJJ Makefile
#
# Local ergonomics only. CI is not in scope for ACT-BJJ-LAB01.
#
# GOCACHE defaults to ./.gocache so the build works even when the
# user's default Go cache location is not writable. Override by
# setting GOCACHE explicitly on the command line.

GO ?= go
GOCACHE ?= $(CURDIR)/.gocache
BIN := $(CURDIR)/bjj

.PHONY: all build test vet run clean run-evidence check-diff

all: check-diff vet test build

check-diff:
	git diff --check

build:
	GOCACHE=$(GOCACHE) $(GO) build -o $(BIN) ./cmd/bjj

test:
	GOCACHE=$(GOCACHE) $(GO) test ./...

vet:
	GOCACHE=$(GOCACHE) $(GO) vet ./...

run: build
	$(BIN) version

run-evidence:
	GOCACHE=$(GOCACHE) $(GO) test ./internal/lab/... -run TestRunEvidenceAllObservationsPass -v

clean:
	rm -f $(BIN)
	rm -rf $(CURDIR)/.gocache
