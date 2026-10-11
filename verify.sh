#!/usr/bin/env bash
set -euo pipefail

if [[ -n "$(gofmt -l *.go)" ]]; then
  echo "Go sources are not gofmt-clean" >&2
  gofmt -l *.go >&2
  exit 1
fi

go test -count=1 ./...
go vet ./...
bash -n build.sh package-release.sh install.sh install_test.sh verify.sh phoenixctl
test -x phoenixctl
bash ./install_test.sh
