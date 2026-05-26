SHELL := /usr/bin/env bash

BUILD_DIR ?= build
TALES_BIN := $(BUILD_DIR)/tales
MOCK_BIN := $(BUILD_DIR)/mockserver
BUILD_READY := .build-ready
GO_GOBIN := $(shell go env GOBIN)
GO_GOPATH := $(shell go env GOPATH)
INSTALL_DIR ?= $(if $(GOBIN),$(GOBIN),$(if $(GO_GOBIN),$(GO_GOBIN),$(GO_GOPATH)/bin))

SKILL_NAME := tales-test-generator
SKILL_SRC := .claude/skills/$(SKILL_NAME)
CLAUDE_SKILLS_DIR ?= $(HOME)/.claude/skills

UNIT_PKGS := ./internal/... ./cmd/tales ./drivers/...

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null || echo none)
GIT_STATE  ?= $(shell test -z "$$(git status --porcelain 2>/dev/null)" && echo clean || echo dirty)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_PKG := github.com/hyperxlab/tales/internal/version
LDFLAGS := -s -w \
	-X $(VERSION_PKG).version=$(VERSION) \
	-X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).gitTreeState=$(GIT_STATE) \
	-X $(VERSION_PKG).buildDate=$(BUILD_DATE)

IOS_DEVICE_NAME ?= iPhone 17
IOS_BUNDLE_ID ?= com.hyperxlab.tales.demo
IOS_DRIVER_HOST ?= 127.0.0.1
IOS_DRIVER_PORT ?= 9080
IOS_DEMO_PROJECT := e2e/ios/demoapp/TalesDemoApp.xcodeproj
IOS_DEMO_SCHEME := TalesDemoApp
IOS_DEMO_DERIVED_DATA := $(BUILD_DIR)/ios/demoapp
IOS_DEMO_APP_PATH_FILE := $(IOS_DEMO_DERIVED_DATA)/app_path.txt
IOS_PASS_SUITE := ./e2e/ios/pass
IOS_FAIL_SUITE := ./e2e/ios/fail

.PHONY: build tales-bin mock-bin install install-skill
build: tales-bin mock-bin

$(BUILD_READY):
	@mkdir -p $(BUILD_DIR)
	@touch $(BUILD_READY)

tales-bin: | $(BUILD_READY)
	@echo "Building $(TALES_BIN) (version=$(VERSION) commit=$(GIT_COMMIT))..."
	@go build -ldflags '$(LDFLAGS)' -o $(TALES_BIN) ./cmd/tales

mock-bin: | $(BUILD_READY)
	@go build -o $(MOCK_BIN) ./e2e/mockserver

install: tales-bin
	@mkdir -p "$(INSTALL_DIR)"
	@install -m 755 "$(TALES_BIN)" "$(INSTALL_DIR)/tales"
	@echo "Installed $(TALES_BIN) to $(INSTALL_DIR)/tales"

install-skill:
	@test -d "$(SKILL_SRC)" || { echo "Skill source not found: $(SKILL_SRC)"; exit 1; }
	@mkdir -p "$(CLAUDE_SKILLS_DIR)"
	@rm -rf "$(CLAUDE_SKILLS_DIR)/$(SKILL_NAME)"
	@cp -R "$(SKILL_SRC)" "$(CLAUDE_SKILLS_DIR)/$(SKILL_NAME)"
	@echo "Installed skill '$(SKILL_NAME)' to $(CLAUDE_SKILLS_DIR)/$(SKILL_NAME)"

.PHONY: test
test:
	@go test -race -count=1 $(UNIT_PKGS)

.PHONY: lint
lint:
	@golangci-lint run ./cmd/tales ./internal/... ./e2e/mockserver ./drivers/...

.PHONY: e2e
e2e: build
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/logs
	@rm -f $(BUILD_DIR)/mockserver.pid
	@set -euo pipefail; \
	( $(MOCK_BIN) > $(BUILD_DIR)/logs/mockserver.log 2>&1 & echo $$! > $(BUILD_DIR)/mockserver.pid ); \
	cleanup() { \
	  if [ -f $(BUILD_DIR)/mockserver.pid ]; then \
	    pid=$$(cat $(BUILD_DIR)/mockserver.pid); \
	    if kill -0 $$pid 2>/dev/null; then kill $$pid; fi; \
	    rm -f $(BUILD_DIR)/mockserver.pid; \
	  fi; \
	}; \
	trap cleanup EXIT INT TERM; \
	for i in $$(seq 1 50); do \
	  if curl -fsS http://localhost:1337/healthz >/dev/null 2>&1; then break; fi; \
	  sleep 0.2; \
	  if [ $$i -eq 50 ]; then echo 'mock server did not start'; exit 1; fi; \
	done; \
	BASE_URL=http://localhost:1337 $(TALES_BIN) test --seed 1234 --parallel 4 --report-junit $(BUILD_DIR)/reports/e2e.junit.xml --report-jsonl $(BUILD_DIR)/reports/e2e.jsonl ./e2e/pass

# SQL e2e infra. Local docker-compose remaps ports (5433/3307) to avoid host
# conflicts; CI uses default ports through GitHub Actions services.
SQL_COMPOSE_FILE ?= docker-compose.yml
LOCAL_POSTGRES_DSN ?= postgres://tales:tales@127.0.0.1:5433/tales?sslmode=disable
LOCAL_MYSQL_DSN    ?= tales:tales@tcp(127.0.0.1:3307)/tales?parseTime=true

.PHONY: sql-up sql-down e2e-sql
sql-up:
	@docker compose -f $(SQL_COMPOSE_FILE) up -d
	@echo "Waiting for postgres + mysql to become healthy..."
	@for i in $$(seq 1 60); do \
	  pg=$$(docker compose -f $(SQL_COMPOSE_FILE) ps --format json postgres | grep -o '"Health":"[^"]*"' | head -n1); \
	  my=$$(docker compose -f $(SQL_COMPOSE_FILE) ps --format json mysql    | grep -o '"Health":"[^"]*"' | head -n1); \
	  if echo "$$pg" | grep -q healthy && echo "$$my" | grep -q healthy; then \
	    echo "postgres + mysql healthy ($${i}s)"; exit 0; \
	  fi; \
	  sleep 1; \
	done; echo "containers did not become healthy"; exit 1

sql-down:
	@docker compose -f $(SQL_COMPOSE_FILE) down -v

e2e-sql: build sql-up
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/logs
	@rm -f $(BUILD_DIR)/mockserver.pid
	@set -euo pipefail; \
	( $(MOCK_BIN) > $(BUILD_DIR)/logs/mockserver.log 2>&1 & echo $$! > $(BUILD_DIR)/mockserver.pid ); \
	cleanup() { \
	  if [ -f $(BUILD_DIR)/mockserver.pid ]; then \
	    pid=$$(cat $(BUILD_DIR)/mockserver.pid); \
	    if kill -0 $$pid 2>/dev/null; then kill $$pid; fi; \
	    rm -f $(BUILD_DIR)/mockserver.pid; \
	  fi; \
	}; \
	trap cleanup EXIT INT TERM; \
	for i in $$(seq 1 50); do \
	  if curl -fsS http://localhost:1337/healthz >/dev/null 2>&1; then break; fi; \
	  sleep 0.2; \
	  if [ $$i -eq 50 ]; then echo 'mock server did not start'; exit 1; fi; \
	done; \
	BASE_URL=http://localhost:1337 \
	POSTGRES_DSN="$(LOCAL_POSTGRES_DSN)" MYSQL_DSN="$(LOCAL_MYSQL_DSN)" \
	  $(TALES_BIN) test --seed 1234 --parallel 4 \
	    --report-junit $(BUILD_DIR)/reports/e2e-sql.junit.xml \
	    --report-jsonl $(BUILD_DIR)/reports/e2e-sql.jsonl \
	    ./e2e/pass

.PHONY: check-ios-host
check-ios-host:
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "iOS targets require macOS with Xcode (got $$(uname -s))."; \
		exit 1; \
	fi
	@command -v xcodebuild >/dev/null 2>&1 || { echo "xcodebuild is required for iOS targets."; exit 1; }
	@command -v xcrun >/dev/null 2>&1 || { echo "xcrun is required for iOS targets."; exit 1; }

.PHONY: clean-ios-driver-cache
clean-ios-driver-cache:
	@rm -rf "$$HOME/Library/Caches/tales/apple-driver"
	@echo "Cleared tales apple-driver cache at \$$HOME/Library/Caches/tales/apple-driver"
	@echo "(set TALES_DRIVER_CACHE_DIR to override the default location.)"

.PHONY: doctor-ios
doctor-ios:
	@set +e; \
	echo "== System =="; \
	sw_vers 2>&1; \
	uname -a 2>&1; \
	echo; \
	echo "== Xcode =="; \
	xcodebuild -version 2>&1; \
	xcode-select -p 2>&1; \
	echo; \
	echo "== simctl =="; \
	xcrun simctl list runtimes 2>&1; \
	xcrun simctl list devicetypes 2>&1; \
	xcrun simctl list devices 2>&1; \
	echo; \
	echo "== Environment =="; \
	echo "IOS_DEVICE_NAME=$${IOS_DEVICE_NAME:-$(IOS_DEVICE_NAME)}"; \
	echo "IOS_APP_PATH=$${IOS_APP_PATH:-}"; \
	echo "IOS_BUNDLE_ID=$${IOS_BUNDLE_ID:-$(IOS_BUNDLE_ID)}"; \
	echo "IOS_DRIVER_HOST=$${IOS_DRIVER_HOST:-$(IOS_DRIVER_HOST)}"; \
	echo "IOS_DRIVER_PORT=$${IOS_DRIVER_PORT:-$(IOS_DRIVER_PORT)}"; \
	echo; \
	echo "If CoreSimulator reports a stale service after an Xcode upgrade, try:"; \
	echo "  sudo xcodebuild -runFirstLaunch"; \
	echo "  xcrun simctl shutdown all"; \
	echo "  killall -9 com.apple.CoreSimulator.CoreSimulatorService || true"; \
	echo "  xcrun simctl list devices"; \
	echo; \
	echo "For a richer view (cache state + Xcode + simctl) once Tales is built, run:"; \
	echo "  ./build/tales doctor       # or 'tales doctor' after make install"; \
	echo "  ./build/tales doctor --json  # machine-readable output for CI"

.PHONY: build-ios-demo
build-ios-demo: check-ios-host | $(BUILD_READY)
	@mkdir -p $(BUILD_DIR)/ios
	@echo "Building iOS demo app for simulator..."
	@xcodebuild \
		-quiet \
		-project $(IOS_DEMO_PROJECT) \
		-scheme $(IOS_DEMO_SCHEME) \
		-configuration Debug \
		-sdk iphonesimulator \
		-derivedDataPath $(IOS_DEMO_DERIVED_DATA) \
		CODE_SIGNING_ALLOWED=NO \
		build
	@app_path=$$(find "$(IOS_DEMO_DERIVED_DATA)" -name 'TalesDemoApp.app' -type d | head -n 1); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	echo "$$app_path" > "$(IOS_DEMO_APP_PATH_FILE)"; \
	echo "Built $$app_path"

.PHONY: e2e-ios
e2e-ios: tales-bin build-ios-demo
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/artifacts
	@set -euo pipefail; \
	app_path=$$(cat "$(IOS_DEMO_APP_PATH_FILE)" 2>/dev/null || true); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	echo "iOS e2e configuration:"; \
	echo "  device:       $(IOS_DEVICE_NAME)"; \
	echo "  app:          $$app_path"; \
	echo "  bundle id:    $(IOS_BUNDLE_ID)"; \
	echo "  driver:       $(IOS_DRIVER_HOST):$(IOS_DRIVER_PORT)"; \
	echo "  JSONL report: $(BUILD_DIR)/reports/e2e-ios.jsonl"; \
	echo "  JUnit report: $(BUILD_DIR)/reports/e2e-ios.junit.xml"; \
	echo "  HTML report:  $(BUILD_DIR)/reports/e2e-ios.html"; \
	echo "  artifacts:    $(BUILD_DIR)/artifacts/mobile"; \
	IOS_APP_PATH="$$app_path" \
	IOS_BUNDLE_ID="$(IOS_BUNDLE_ID)" \
	IOS_DEVICE_NAME="$(IOS_DEVICE_NAME)" \
	IOS_DRIVER_HOST="$(IOS_DRIVER_HOST)" \
	IOS_DRIVER_PORT="$(IOS_DRIVER_PORT)" \
	$(TALES_BIN) test --seed 1234 --parallel 1 \
		--report-junit $(BUILD_DIR)/reports/e2e-ios.junit.xml \
		--report-jsonl $(BUILD_DIR)/reports/e2e-ios.jsonl \
		--report-html $(BUILD_DIR)/reports/e2e-ios.html \
		--capture-screenshots actions \
		$(IOS_PASS_SUITE) || { status=$$?; echo "iOS e2e failed. Run \`make doctor-ios\` for diagnostics."; exit $$status; }; \
	scripts/verify-ios-visual.sh "$(BUILD_DIR)/reports/e2e-ios.html" || { echo "visual report verification failed."; exit 1; }

.PHONY: e2e-ios-failure
e2e-ios-failure: tales-bin build-ios-demo
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/artifacts
	@rm -rf $(BUILD_DIR)/artifacts/mobile
	@set -euo pipefail; \
	app_path=$$(cat "$(IOS_DEMO_APP_PATH_FILE)" 2>/dev/null || true); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	echo "iOS failure e2e configuration:"; \
	echo "  device:       $(IOS_DEVICE_NAME)"; \
	echo "  app:          $$app_path"; \
	echo "  bundle id:    $(IOS_BUNDLE_ID)"; \
	echo "  driver:       $(IOS_DRIVER_HOST):$(IOS_DRIVER_PORT)"; \
	echo "  JSONL report: $(BUILD_DIR)/reports/e2e-ios-failure.jsonl"; \
	echo "  JUnit report: $(BUILD_DIR)/reports/e2e-ios-failure.junit.xml"; \
	echo "  HTML report:  $(BUILD_DIR)/reports/e2e-ios-failure.html"; \
	echo "  artifacts:    $(BUILD_DIR)/artifacts/mobile"; \
	set +e; \
	IOS_APP_PATH="$$app_path" \
	IOS_BUNDLE_ID="$(IOS_BUNDLE_ID)" \
	IOS_DEVICE_NAME="$(IOS_DEVICE_NAME)" \
	IOS_DRIVER_HOST="$(IOS_DRIVER_HOST)" \
	IOS_DRIVER_PORT="$(IOS_DRIVER_PORT)" \
	$(TALES_BIN) test --seed 1234 --parallel 1 \
		--report-junit $(BUILD_DIR)/reports/e2e-ios-failure.junit.xml \
		--report-jsonl $(BUILD_DIR)/reports/e2e-ios-failure.jsonl \
		--report-html $(BUILD_DIR)/reports/e2e-ios-failure.html \
		--capture-screenshots actions \
		$(IOS_FAIL_SUITE); \
	exit_code=$$?; \
	set -e; \
	if [ $$exit_code -ne 1 ]; then echo "expected Tales to exit 1, got $$exit_code"; exit 1; fi; \
	scripts/verify-ios-failure.sh "$(BUILD_DIR)/reports/e2e-ios-failure.jsonl" "$(BUILD_DIR)/artifacts/mobile" || { echo "iOS failure verification failed. Run \`make doctor-ios\` for diagnostics."; exit 1; }; \
	scripts/verify-ios-visual.sh "$(BUILD_DIR)/reports/e2e-ios-failure.html" || { echo "visual report verification failed."; exit 1; }

.PHONY: e2e-failure
e2e-failure: build
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/logs
	@rm -f $(BUILD_DIR)/mockserver.pid
	@set -euo pipefail; \
	( $(MOCK_BIN) > $(BUILD_DIR)/logs/mockserver.log 2>&1 & echo $$! > $(BUILD_DIR)/mockserver.pid ); \
	cleanup() { \
	  if [ -f $(BUILD_DIR)/mockserver.pid ]; then \
	    pid=$$(cat $(BUILD_DIR)/mockserver.pid); \
	    if kill -0 $$pid 2>/dev/null; then kill $$pid; fi; \
	    rm -f $(BUILD_DIR)/mockserver.pid; \
	  fi; \
	}; \
	trap cleanup EXIT INT TERM; \
	for i in $$(seq 1 50); do \
	  if curl -fsS http://localhost:1337/healthz >/dev/null 2>&1; then break; fi; \
	  sleep 0.2; \
	  if [ $$i -eq 50 ]; then echo 'mock server did not start'; exit 1; fi; \
	done; \
	set +e; \
	BASE_URL=http://localhost:1337 $(TALES_BIN) test --seed 1234 --parallel 1 --report-jsonl $(BUILD_DIR)/reports/e2e-failure.jsonl ./e2e/fail; \
	exit_code=$$?; \
	set -e; \
	test $$exit_code -eq 1

.PHONY: release-check
release-check:
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required (https://goreleaser.com/install/)"; exit 1; }
	@goreleaser check

.PHONY: release-snapshot
release-snapshot:
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required (https://goreleaser.com/install/)"; exit 1; }
	@goreleaser release --snapshot --clean

.PHONY: release-build
release-build:
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required (https://goreleaser.com/install/)"; exit 1; }
	@goreleaser build --snapshot --clean
