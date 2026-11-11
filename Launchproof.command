#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$project_dir"
if [ -x ./.tools/go/bin/go ]; then
  PATH="$project_dir/.tools/go/bin:$PATH"
  export PATH
fi
if [ ! -x ./bin/launchproof ]; then
	go_command=go
	if [ -x ./.tools/go/bin/go ]; then
		go_command=./.tools/go/bin/go
	fi
  if ! command -v "$go_command" >/dev/null 2>&1; then
    printf 'Go is required for the first build. Install Go 1.26.7 or newer.\n'
    exit 1
  fi
  mkdir -p bin
  "$go_command" build -o bin/launchproof ./cmd/launchproof
fi
exec ./bin/launchproof
