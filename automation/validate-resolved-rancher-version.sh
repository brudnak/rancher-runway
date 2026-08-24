#!/usr/bin/env bash

set -euo pipefail

requested="${1:-}"
resolved="${2:-}"

trim() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

normalize() {
  local value
  value="$(trim "$1")"
  value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  if [[ "$value" =~ ^[0-9] ]]; then
    value="v$value"
  fi
  printf '%s' "$value"
}

fail() {
  printf 'invalid resolved Rancher target: %s\n' "$1" >&2
  exit 1
}

requested="$(normalize "$requested")"
resolved="$(normalize "$resolved")"

if [[ -z "$requested" ]]; then
  fail "rancher_version is empty"
fi

# A blank resolution is intentional for direct workflow_dispatch runs. The lane
# planner will validate and, when needed, resolve the requested target itself.
if [[ -z "$resolved" ]]; then
  exit 0
fi

immutable_head_re='^v([0-9]+)\.([0-9]+)(\.([0-9]+))?-([0-9a-f]{7,40})-head$'

if [[ "$requested" == "head" ]]; then
  if [[ ! "$resolved" =~ $immutable_head_re ]]; then
    fail "head must resolve to an immutable vMAJOR.MINOR[- or .PATCH-]SHA-head target, got '$resolved'"
  fi
  exit 0
fi

if [[ "$requested" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)-head$ ]]; then
  requested_major="${BASH_REMATCH[1]}"
  requested_minor="${BASH_REMATCH[2]}"
  requested_patch="${BASH_REMATCH[3]}"
  if [[ "$resolved" =~ $immutable_head_re ]]; then
    resolved_major="${BASH_REMATCH[1]}"
    resolved_minor="${BASH_REMATCH[2]}"
    resolved_patch="${BASH_REMATCH[4]:-}"
  else
    fail "patch alias '$requested' must resolve to an immutable vMAJOR.MINOR.PATCH-SHA-head target, got '$resolved'"
  fi
  if [[ -z "$resolved_patch" || "$resolved_major" != "$requested_major" || "$resolved_minor" != "$requested_minor" || "$resolved_patch" != "$requested_patch" ]]; then
    fail "patch alias '$requested' resolved outside its exact patch: '$resolved'"
  fi
  exit 0
fi

if [[ "$requested" =~ ^v([0-9]+)\.([0-9]+)-head$ ]]; then
  requested_major="${BASH_REMATCH[1]}"
  requested_minor="${BASH_REMATCH[2]}"
  if [[ "$resolved" =~ $immutable_head_re ]]; then
    resolved_major="${BASH_REMATCH[1]}"
    resolved_minor="${BASH_REMATCH[2]}"
  else
    fail "minor alias '$requested' must resolve to an immutable vMAJOR.MINOR[- or .PATCH-]SHA-head target, got '$resolved'"
  fi
  if [[ "$resolved_major" != "$requested_major" || "$resolved_minor" != "$requested_minor" ]]; then
    fail "minor alias '$requested' resolved outside its release line: '$resolved'"
  fi
  exit 0
fi

# Immutable heads and release targets are already fully selected. A parent may
# repeat them as the resolved value, but it may not silently substitute another
# target. Normalization above permits only an optional leading v and hex case.
if [[ "$resolved" != "$requested" ]]; then
  fail "immutable or release target '$requested' must resolve to itself, got '$resolved'"
fi
