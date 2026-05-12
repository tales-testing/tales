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

UNIT_PKGS := ./internal/... ./cmd/tales

IOS_DEVICE_NAME ?= iPhone 16
IOS_BUNDLE_ID ?= com.hyperxlab.tales.demo
IOS_DEMO_PROJECT := e2e/ios/demoapp/TalesDemoApp.xcodeproj
IOS_DEMO_SCHEME := TalesDemoApp
IOS_DEMO_DERIVED_DATA := $(BUILD_DIR)/ios/demoapp
IOS_PASS_SUITE := ./e2e/ios/pass
IOS_FAIL_SUITE := ./e2e/ios/fail

.PHONY: build tales-bin mock-bin install install-skill
build: tales-bin mock-bin

$(BUILD_READY):
	@mkdir -p $(BUILD_DIR)
	@touch $(BUILD_READY)

tales-bin: | $(BUILD_READY)
	@echo "Building $(TALES_BIN)..."
	@go build -o $(TALES_BIN) ./cmd/tales

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
	@golangci-lint run ./cmd/tales ./internal/... ./e2e/mockserver

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

.PHONY: check-ios-host
check-ios-host:
	@if [ "$$(uname -s)" != "Darwin" ]; then \
		echo "iOS targets require macOS with Xcode (got $$(uname -s))."; \
		exit 1; \
	fi
	@command -v xcodebuild >/dev/null 2>&1 || { echo "xcodebuild is required for iOS targets."; exit 1; }
	@command -v xcrun >/dev/null 2>&1 || { echo "xcrun is required for iOS targets."; exit 1; }

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
		-destination "generic/platform=iOS Simulator" \
		-derivedDataPath $(IOS_DEMO_DERIVED_DATA) \
		CODE_SIGNING_ALLOWED=NO \
		build
	@app_path=$$(find "$(IOS_DEMO_DERIVED_DATA)" -name 'TalesDemoApp.app' -type d | head -n 1); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	echo "Built $$app_path"

.PHONY: e2e-ios
e2e-ios: tales-bin build-ios-demo
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/artifacts
	@set -euo pipefail; \
	app_path=$$(find "$(IOS_DEMO_DERIVED_DATA)" -name 'TalesDemoApp.app' -type d | head -n 1); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	IOS_APP_PATH="$$app_path" \
	IOS_BUNDLE_ID="$(IOS_BUNDLE_ID)" \
	IOS_DEVICE_NAME="$(IOS_DEVICE_NAME)" \
	$(TALES_BIN) test --seed 1234 --parallel 1 \
		--report-junit $(BUILD_DIR)/reports/e2e-ios.junit.xml \
		--report-jsonl $(BUILD_DIR)/reports/e2e-ios.jsonl \
		$(IOS_PASS_SUITE)

.PHONY: e2e-ios-failure
e2e-ios-failure: tales-bin build-ios-demo
	@mkdir -p $(BUILD_DIR)/reports $(BUILD_DIR)/artifacts
	@rm -rf $(BUILD_DIR)/artifacts/mobile
	@set -euo pipefail; \
	app_path=$$(find "$(IOS_DEMO_DERIVED_DATA)" -name 'TalesDemoApp.app' -type d | head -n 1); \
	if [ -z "$$app_path" ]; then echo "TalesDemoApp.app was not produced under $(IOS_DEMO_DERIVED_DATA)."; exit 1; fi; \
	set +e; \
	IOS_APP_PATH="$$app_path" \
	IOS_BUNDLE_ID="$(IOS_BUNDLE_ID)" \
	IOS_DEVICE_NAME="$(IOS_DEVICE_NAME)" \
	$(TALES_BIN) test --seed 1234 --parallel 1 \
		--report-junit $(BUILD_DIR)/reports/e2e-ios-failure.junit.xml \
		--report-jsonl $(BUILD_DIR)/reports/e2e-ios-failure.jsonl \
		$(IOS_FAIL_SUITE); \
	exit_code=$$?; \
	set -e; \
	test $$exit_code -eq 1; \
	test -n "$$(find $(BUILD_DIR)/artifacts/mobile -name screenshot.png -type f | head -n 1)" || { echo "missing iOS screenshot artifact"; exit 1; }; \
	test -n "$$(find $(BUILD_DIR)/artifacts/mobile -name hierarchy.json -type f | head -n 1)" || { echo "missing iOS hierarchy artifact"; exit 1; }; \
	grep -q '"artifacts"' $(BUILD_DIR)/reports/e2e-ios-failure.jsonl || { echo "iOS failure JSONL does not include artifact paths"; exit 1; }

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
