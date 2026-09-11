.PHONY: build install clean test vet lint docker help check-search-ui
.DEFAULT_GOAL := build

BINARY := puff

DOCKER_IMAGE ?= hevmind/puff-exporter
DOCKER_TAG   ?= dev

check-search-ui:
	python3 scripts/check-search-ui.py

build:
	go build -o $(BINARY) .

install:
	go install .

test:
	go test ./... -race -count=1

vet:
	go vet ./...

lint:
	golangci-lint run ./...

docker: build
	docker build -f Dockerfile.exporter -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

clean:
	rm -f $(BINARY)

help:
	@echo "Available targets:"
	@echo "  build      - Build the puff binary"
	@echo "  install    - Install to GOPATH/bin"
	@echo "  test       - Run unit tests with race detector"
	@echo "  vet        - Run go vet"
	@echo "  lint       - Run golangci-lint"
	@echo "  docker     - Build the exporter Docker image ($(DOCKER_IMAGE):$(DOCKER_TAG))"
	@echo "  clean      - Remove build artifacts"
