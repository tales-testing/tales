BUILD_DIR ?= build
COMMIT = $(shell git rev-parse HEAD)
VERSION ?= $(shell git describe --always --tags --dirty)
ORG := github.com/hyperxlab
PROJECT := tales
REPOPATH ?= $(ORG)/$(PROJECT)
VERSION_PACKAGE = $(REPOPATH)/pkg/tales/version

GO_LDFLAGS :="
GO_LDFLAGS += -X $(VERSION_PACKAGE).version=$(VERSION)
GO_LDFLAGS += -X $(VERSION_PACKAGE).buildDate=$(shell date +'%Y-%m-%dT%H:%M:%SZ')
GO_LDFLAGS += -X $(VERSION_PACKAGE).gitCommit=$(COMMIT)
GO_LDFLAGS += -X $(VERSION_PACKAGE).gitTreeState=$(if $(shell git status --porcelain),dirty,clean)
GO_LDFLAGS +="

GO_FILES := $(shell find . -type f -name '*.go' -not -path "./vendor/*")

.PHONY: all
all: deps build test

.PHONY: deps
deps:
	@go mod download

.PHONY: clean
clean:
	@go clean -i ./...

_build:
	@mkdir -p ${BUILD_DIR}

$(BUILD_DIR)/coverage.out: _build $(GO_FILES)
	@go test -cover -race -coverprofile $(BUILD_DIR)/coverage.out.tmp -timeout 300s ./...
	@cat $(BUILD_DIR)/coverage.out.tmp | grep -v '.pb.go' | grep -v 'mock_' | grep -v 'bindata.go' > $(BUILD_DIR)/coverage.out
	@rm $(BUILD_DIR)/coverage.out.tmp

ci-test: _build
ifeq (, $(shell which go2xunit))
	@echo "Install go2xunit..."
	@go get github.com/tebeka/go2xunit
endif
	@mkdir -p ./test-results
	@go test -race -timeout 300s -cover -coverprofile $(BUILD_DIR)/coverage.out.tmp -v ./... | go2xunit -fail -output ./test-results/tests.xml
	@cat $(BUILD_DIR)/coverage.out.tmp | grep -v '.pb.go' | grep -v 'mock_' > $(BUILD_DIR)/coverage.out
	@rm $(BUILD_DIR)/coverage.out.tmp
	@echo ""
	@go tool cover -func $(BUILD_DIR)/coverage.out

.PHONY: lint
lint:
ifeq (, $(shell which golangci-lint))
	@echo "Install golangci-lint..."
	@curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b ${GOPATH}/bin v1.20.0
endif
	@echo "lint..."
	@golangci-lint run --timeout=300s ./...

.PHONY: test
test: $(BUILD_DIR)/coverage.out

.PHONY: coverage
coverage: $(BUILD_DIR)/coverage.out
	@echo ""
	@go tool cover -func ./$(BUILD_DIR)/coverage.out

.PHONY: coverage-html
coverage-html: $(BUILD_DIR)/coverage.out
	@go tool cover -html ./$(BUILD_DIR)/coverage.out

generate: $(GO_FILES)
	@go generate ./...

${BUILD_DIR}/acme-api-server: $(GO_FILES) go.mod go.sum
	@echo "Building $@..."
	@go generate ./cmd/$(subst ${BUILD_DIR}/,,$@)/
	@go build -ldflags $(GO_LDFLAGS) -o $@ ./cmd/$(subst ${BUILD_DIR}/,,$@)/


${BUILD_DIR}/tales: $(GO_FILES) go.mod go.sum
	@echo "Building $@..."
	@go generate ./cmd/$(subst ${BUILD_DIR}/,,$@)/
	@go build -ldflags $(GO_LDFLAGS) -o $@ ./cmd/$(subst ${BUILD_DIR}/,,$@)/


${BUILD_DIR}/tales-v1: $(GO_FILES) go.mod go.sum
	@echo "Building $@..."
	@go generate ./cmd/$(subst ${BUILD_DIR}/,,$@)/
	@go build -ldflags $(GO_LDFLAGS) -o $@ ./cmd/$(subst ${BUILD_DIR}/,,$@)/


.PHONY: run-acme-api-server
run-acme-api-server: ${BUILD_DIR}/acme-api-server
	@echo "Running $<..."
	@./$<

.PHONY: run-tales
run-tales: ${BUILD_DIR}/tales
	@echo "Running $<..."
	@./$<

run: run-tales

.PHONY: build
build: ${BUILD_DIR}/acme-api-server ${BUILD_DIR}/tales ${BUILD_DIR}/tales-v1

run-dev: ${BUILD_DIR}/tales
	@./$< dev/api-v2.tales

run-dev-v1: ${BUILD_DIR}/tales-v1
	@cd dev; ../$<
