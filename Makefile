SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c

APP_NAME ?= Rancher HA RKE2
INSTALL_DIR ?= /Applications
APP_PATH := $(INSTALL_DIR)/$(APP_NAME).app
STATUS_JSON := terratest/automation-output/install-status.json

.PHONY: help setup install app build panel-css check-install-safe check-app-closed check-lifecycle-idle test

help:
	@printf '%s\n' "Targets:"
	@printf '  %-20s %s\n' "make setup" "Check local safety, rebuild, and install $(APP_NAME).app"
	@printf '  %-20s %s\n' "make install" "Alias for setup"
	@printf '  %-20s %s\n' "make app" "Build the Wails app without installing it"
	@printf '  %-20s %s\n' "make panel-css" "Rebuild the embedded control-panel CSS"
	@printf '  %-20s %s\n' "make test" "Run Go tests"

install: setup

setup: check-install-safe
	@echo "Building and replacing $(APP_PATH)"
	@HA_RANCHER_APP_NAME="$(APP_NAME)" HA_RANCHER_INSTALL_DIR="$(INSTALL_DIR)" scripts/install.sh

app:
	@scripts/build-wails-app.sh

build: app

panel-css:
	@npm run build:panel-css

test:
	@go test ./...

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
	if ! go run ./cmd/ha-rancher status -json > "$(STATUS_JSON).tmp"; then \
	  rm -f "$(STATUS_JSON).tmp"; \
	  echo "Could not inspect local lifecycle status; continuing with app install checks only." >&2; \
	  exit 0; \
	fi; \
	mv "$(STATUS_JSON).tmp" "$(STATUS_JSON)"; \
	node scripts/check-lifecycle-idle.js "$(STATUS_JSON)"
