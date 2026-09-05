#!/bin/sh
# Fail the agent done-gate unless listed packages (or ./...) pass.
set -eu
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
if [ "$#" -eq 0 ]; then
	go test ./...
	exit 0
fi
go test "$@"
