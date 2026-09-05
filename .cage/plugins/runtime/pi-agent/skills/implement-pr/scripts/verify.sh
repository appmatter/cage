#!/bin/sh
# Fail the agent done-gate unless listed packages (or ./...) pass.
set -eu
root=$PWD
while [ "$root" != "/" ] && [ ! -f "$root/go.mod" ]; do
	root=$(dirname "$root")
done
if [ ! -f "$root/go.mod" ]; then
	echo "verify.sh: no go.mod above $PWD" >&2
	exit 1
fi
cd "$root"
if [ "$#" -eq 0 ]; then
	go test ./...
	exit 0
fi
go test "$@"
