#!/usr/bin/env bash
set -euo pipefail
./verify.sh
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -buildmode=c-shared -o cpa-phoenix.dylib .
