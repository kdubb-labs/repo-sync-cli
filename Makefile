.PHONY: build fmt test vet ci install clean

BINARY := bin/repo-sync
INSTALL_DIR := $(HOME)/.local/bin

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/repo-sync

fmt:
	gofmt -w cmd internal

test:
	go test ./...

vet:
	go vet ./...

ci: fmt test vet build

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/repo-sync
	$(INSTALL_DIR)/repo-sync agent-context >/dev/null

clean:
	rm -rf bin
