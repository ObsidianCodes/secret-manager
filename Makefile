BINARY  := secretman
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/ObsidianCodes/secret-manager/cmd.Version=$(VERSION)

.PHONY: build install test vet fmt check clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# fmt is checked, not applied: a formatting fix landing silently in a security
# tool's diff is one more thing a reviewer has to read past.
check: vet test
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "ok"

clean:
	rm -rf bin
