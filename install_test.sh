#!/usr/bin/env bash
set -euo pipefail

# Exercise the installer with a fake shared library and temporary CPA config.
# This test never starts CPA and never touches an account directory.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT

mkdir -p "${fixture}/home"
cp "${script_dir}/install.sh" "${fixture}/install.sh"
printf 'synthetic cpa-phoenix library\n' > "${fixture}/cpa-phoenix.dylib"
cat > "${fixture}/config.yaml" <<EOF
plugins:
  enabled: true
  dir: "${fixture}/configured-plugins"
EOF

run_installer() {
  HOME="${fixture}/home" bash "${fixture}/install.sh" "$@"
}

before="$(run_installer --config "${fixture}/config.yaml" --check)"
grep -F "Destination: ${fixture}/configured-plugins/cpa-phoenix.dylib (not installed)" <<<"${before}" >/dev/null
grep -F "CPA plugin setting: enabled" <<<"${before}" >/dev/null
grep -F "Next step: Run bash ./install.sh" <<<"${before}" >/dev/null

installed="$(run_installer --config "${fixture}/config.yaml")"
grep -F "Installed: ${fixture}/configured-plugins/cpa-phoenix.dylib" <<<"${installed}" >/dev/null
grep -F "Next step: Restart CLIProxyAPI, then open the Phoenix dashboard." <<<"${installed}" >/dev/null
[[ -f "${fixture}/configured-plugins/cpa-phoenix.dylib" ]]
[[ ! -e "${fixture}/configured-plugins/cpa-phoenix.dylib.bak" ]]

after="$(run_installer --config "${fixture}/config.yaml" --check)"
grep -F "Destination: ${fixture}/configured-plugins/cpa-phoenix.dylib (current)" <<<"${after}" >/dev/null
grep -F "Next step: Restart CLIProxyAPI, then open the Phoenix dashboard." <<<"${after}" >/dev/null

override="${fixture}/override-plugins"
mismatch="$(run_installer --config "${fixture}/config.yaml" --plugin-dir "${override}")"
grep -F "Installed: ${override}/cpa-phoenix.dylib" <<<"${mismatch}" >/dev/null
grep -F "Set plugins.dir to ${override} in ${fixture}/config.yaml" <<<"${mismatch}" >/dev/null

next="$(run_installer --config "${fixture}/config.yaml" --print-next-step)"
grep -F "Restart CLIProxyAPI, then open the Phoenix dashboard." <<<"${next}" >/dev/null

# CPA's native loader uses a platform/architecture child when it exists.
mkdir -p "${fixture}/configured-plugins/darwin/arm64"
platform_install="$(run_installer --config "${fixture}/config.yaml")"
grep -F "Installed: ${fixture}/configured-plugins/darwin/arm64/cpa-phoenix.dylib" <<<"${platform_install}" >/dev/null
[[ -f "${fixture}/configured-plugins/darwin/arm64/cpa-phoenix.dylib" ]]

echo "install_test: ok"
