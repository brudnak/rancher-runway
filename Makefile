SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

APP_NAME ?= Rancher Runway
INSTALL_DIR ?= /Applications
APP_PATH := $(INSTALL_DIR)/$(APP_NAME).app
STATUS_JSON := terratest/automation-output/install-status.json
WAILS_FRONTEND_DIR := desktop/wails/frontend

.PHONY: help setup install app build node-deps frontend-deps panel-css panel-vue panel-ui check-install-safe check-app-closed check-lifecycle-idle test ci ci-go ci-web ci-terraform ci-workflows

help:
	@printf '%s\n' "Targets:"
	@printf '  %-20s %s\n' "make setup" "Check local safety, rebuild, and install $(APP_NAME).app"
	@printf '  %-20s %s\n' "make install" "Alias for setup"
	@printf '  %-20s %s\n' "make app" "Build the Wails app without installing it"
	@printf '  %-20s %s\n' "make panel-ui" "Rebuild embedded control-panel CSS and Vue assets"
	@printf '  %-20s %s\n' "make test" "Run Go tests"
	@printf '  %-20s %s\n' "make ci" "Run local CI checks"

install: setup

setup: check-install-safe
	@echo "Building and replacing $(APP_PATH)"
	@RANCHER_RUNWAY_APP_NAME="$(APP_NAME)" RANCHER_RUNWAY_INSTALL_DIR="$(INSTALL_DIR)" scripts/install.sh

app:
	@scripts/build-wails-app.sh

build: app

node-deps:
	@npm install

frontend-deps:
	@npm --prefix "$(WAILS_FRONTEND_DIR)" install

panel-css: node-deps
	@npm run build:panel-css

panel-vue: node-deps
	@npm run build:panel-vue

panel-ui: node-deps
	@npm run build:panel-ui

test:
	@go test ./...

ci: ci-go ci-web ci-terraform ci-workflows

ci-go:
	@go test ./...

ci-web: panel-ui frontend-deps
	@npm --prefix "$(WAILS_FRONTEND_DIR)" run build

ci-terraform:
	@terraform -chdir=modules/aws fmt -check -recursive
	@terraform -chdir=bootstrap/terraform-state fmt -check -recursive
	@terraform -chdir=modules/aws init -backend=false -input=false -no-color
	@terraform -chdir=modules/aws validate -no-color
	@terraform -chdir=bootstrap/terraform-state init -backend=false -input=false -no-color
	@terraform -chdir=bootstrap/terraform-state validate -no-color

ci-workflows:
	@if command -v actionlint >/dev/null 2>&1; then \
	  actionlint .github/workflows/*.yml; \
	else \
	  go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 .github/workflows/*.yml; \
	fi

check-install-safe: check-app-closed check-lifecycle-idle

check-app-closed:
	@running="false"; \
	if command -v osascript >/dev/null 2>&1; then \
	  running="$$(osascript -e 'application "$(APP_NAME)" is running' 2>/dev/null || printf 'false')"; \
	fi; \
	if [[ "$${running}" != "true" ]] && command -v pgrep >/dev/null 2>&1; then \
	  if pgrep -f "$$(printf '%s' '$(APP_PATH)' | sed 's/[.[\*^$$()+?{}|]/\\&/g')" >/dev/null 2>&1 || \
	     pgrep -f "$$(printf '%s' 'Contents/MacOS/$(APP_NAME)' | sed 's/[.[\*^$$()+?{}|]/\\&/g')" >/dev/null 2>&1; then \
	    running="true"; \
	  fi; \
	fi; \
	if [[ "$${running}" == "true" ]]; then \
	  message="$(APP_NAME) is currently open. Quit the app and wait for any active setup, readiness, or cleanup run to finish before reinstalling."; \
	  echo "$${message}" >&2; \
	  if command -v osascript >/dev/null 2>&1; then osascript -e "display alert \"$(APP_NAME) is open\" message \"$${message}\" as warning" >/dev/null 2>&1 || true; fi; \
	  exit 1; \
	fi

check-lifecycle-idle:
	@mkdir -p "$$(dirname "$(STATUS_JSON)")"; \
	if ! go run ./cmd/rancher-runway status -json > "$(STATUS_JSON).tmp"; then \
	  rm -f "$(STATUS_JSON).tmp"; \
	  echo "Could not inspect local lifecycle status; continuing with app install checks only." >&2; \
	  exit 0; \
	fi; \
	mv "$(STATUS_JSON).tmp" "$(STATUS_JSON)"; \
	node scripts/check-lifecycle-idle.js "$(STATUS_JSON)"
