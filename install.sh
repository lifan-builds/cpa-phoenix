#!/usr/bin/env bash
set -euo pipefail

# Install the already-built Phoenix library into the directory used by
# CLIProxyAPI. The script does not restart CPA; it prints the next step.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
artifact="${script_dir}/cpa-phoenix.dylib"
plugin_dir=""
config_path=""
dry_run=0
check_only=0
print_next_step=0
config_explicit=0

usage() {
  cat <<'EOF'
Usage: bash ./install.sh [options]

Install cpa-phoenix.dylib into the CLIProxyAPI plugin directory.

Options:
  --plugin-dir PATH  Install into PATH instead of reading plugins.dir.
  --config PATH      Read plugins.dir from PATH.
  --check            Print the resolved install and CPA settings without changing files.
  --dry-run          Show the resolved paths without changing files.
  --print-next-step  Print only the next action for the resolved install.
  -h, --help         Show this help.

The script does not restart CLIProxyAPI. After installing, restart CPA and open
the Phoenix dashboard so CPA can load the library.
EOF
}

while (($#)); do
  case "$1" in
    --plugin-dir)
      [[ $# -ge 2 ]] || { echo "--plugin-dir requires a path" >&2; exit 2; }
      plugin_dir="$2"
      shift 2
      ;;
    --config)
      [[ $# -ge 2 ]] || { echo "--config requires a path" >&2; exit 2; }
      config_path="$2"
      config_explicit=1
      shift 2
      ;;
    --check)
      check_only=1
      shift
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    --print-next-step)
      print_next_step=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "${config_path}" ]]; then
  config_path="${CPA_CONFIG_PATH:-${CPA_CONFIG_FILE:-}}"
fi
if [[ -z "${config_path}" ]]; then
  for candidate in \
    "${HOME}/.cli-proxy-api/config.yaml" \
    "${HOME}/config.yaml" \
    "${PWD}/config.yaml"; do
    if [[ -f "${candidate}" ]]; then
      config_path="${candidate}"
      break
    fi
  done
fi

if ((config_explicit)) && [[ ! -f "${config_path}" && -z "${plugin_dir}" ]]; then
  echo "Config file not found: ${config_path}" >&2
  echo "Pass --plugin-dir PATH if your CLIProxyAPI config lives elsewhere." >&2
  exit 1
fi

read_plugins_field() {
  local wanted="$1"
  local path="$2"
  awk -v wanted="${wanted}" '
    function indent(line) { match(line, /^[[:space:]]*/); return RLENGTH }
    function key(line, value) {
      value=line
      sub(/^[[:space:]]*/, "", value)
      sub(/:.*/, "", value)
      gsub(/"/, "", value)
      gsub(/[[:space:]]/, "", value)
      return value
    }
    key($0) == "plugins" { base=indent($0); child_indent=0; active=1; next }
    active && $0 !~ /^[[:space:]]*$/ && indent($0) <= base { active=0 }
    active && child_indent == 0 && indent($0) > base { child_indent=indent($0) }
    active && indent($0) == child_indent && key($0) == wanted {
      value=$0
      sub("^[^:]*:[[:space:]]*", "", value)
      sub(/[[:space:]]+#.*$/, "", value)
      sub(/^"/, "", value)
      sub(/"$/, "", value)
      sub(/^\047/, "", value)
      sub(/\047$/, "", value)
      print value
      exit
    }
  ' "${path}"
}

plugin_dir_source="explicit option"
if [[ -z "${plugin_dir}" && -n "${config_path}" && -f "${config_path}" ]]; then
  # Read only the scalar dir directly under the plugins mapping. This avoids
  # evaluating YAML as shell code and leaves an explicit override available for
  # unusual configs.
  plugin_dir="$(read_plugins_field dir "${config_path}")"
  if [[ -n "${plugin_dir}" ]]; then
    plugin_dir_source="CPA config plugins.dir"
  fi
fi

if [[ -z "${plugin_dir}" ]]; then
  plugin_dir="${HOME}/.cli-proxy-api/plugins"
  plugin_dir_source="default ${HOME}/.cli-proxy-api/plugins"
fi

# Support the common config spelling without invoking eval.
plugin_dir="${plugin_dir//\$HOME/${HOME}}"
plugin_dir="${plugin_dir//\$\{HOME\}/${HOME}}"
plugin_dir="${plugin_dir/#\~\//${HOME}/}"
plugin_dir="${plugin_dir%/}"
if [[ -z "${plugin_dir}" || "${plugin_dir}" == *"/absolute/path/"* ]]; then
  echo "Could not resolve a usable plugin directory. Pass --plugin-dir PATH." >&2
  exit 1
fi

configured_plugin_dir=""
if [[ -n "${config_path}" && -f "${config_path}" ]]; then
  configured_plugin_dir="$(read_plugins_field dir "${config_path}")"
  configured_plugin_dir="${configured_plugin_dir//\$HOME/${HOME}}"
  configured_plugin_dir="${configured_plugin_dir//\$\{HOME\}/${HOME}}"
  configured_plugin_dir="${configured_plugin_dir/#\~\//${HOME}/}"
  configured_plugin_dir="${configured_plugin_dir%/}"
fi

# CPA keeps native plugins below a platform/architecture directory when that
# directory already exists. Keep the configured root as the source of truth,
# but install beside the other loaded dylibs so a restart actually activates
# the new build. Plain directories (including test fixtures) stay unchanged.
plugin_root="${plugin_dir}"
platform_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
architecture="$(uname -m)"
case "${architecture}" in
  x86_64) architecture="amd64" ;;
  aarch64) architecture="arm64" ;;
esac
platform_dir="${plugin_root}/${platform_name}/${architecture}"
if [[ "$(basename "${plugin_root}")" != "${architecture}" && -d "${platform_dir}" ]]; then
  plugin_dir="${platform_dir}"
  plugin_dir_source="${plugin_dir_source} (${platform_name}/${architecture})"
fi

destination="${plugin_dir}/cpa-phoenix.dylib"
config_dir_match="unknown"
if [[ -n "${configured_plugin_dir}" ]]; then
  if [[ "${configured_plugin_dir}" == "${plugin_root}" ]]; then
    config_dir_match="yes"
  else
    config_dir_match="no"
  fi
fi
config_state="not provided"
plugin_enabled="not set"
if [[ -n "${config_path}" ]]; then
  if [[ -f "${config_path}" ]]; then
    config_state="found"
    configured_enabled="$(read_plugins_field enabled "${config_path}")"
    if [[ -n "${configured_enabled}" ]]; then
      normalized_enabled="$(printf '%s' "${configured_enabled}" | tr '[:upper:]' '[:lower:]')"
      case "${normalized_enabled}" in
        true|yes|on|1) plugin_enabled="enabled" ;;
        false|no|off|0) plugin_enabled="disabled" ;;
        *) plugin_enabled="set to ${configured_enabled}" ;;
      esac
    fi
  else
    config_state="not found"
  fi
fi

artifact_state="missing"
if [[ -f "${artifact}" ]]; then
  artifact_state="found"
fi
destination_state="not installed"
if [[ -f "${destination}" ]]; then
  if cmp -s "${artifact}" "${destination}" 2>/dev/null; then
    destination_state="current"
  else
    destination_state="different library already installed"
  fi
fi

next_step() {
  if [[ "${artifact_state}" != "found" ]]; then
    echo "Run ./build.sh, then run bash ./install.sh."
  elif [[ "${destination_state}" == "not installed" || "${destination_state}" == "different library already installed" ]]; then
    echo "Run bash ./install.sh to place Phoenix in ${plugin_dir}."
  elif [[ "${config_dir_match}" == "no" ]]; then
    echo "Set plugins.dir to ${plugin_dir} in ${config_path}, then restart CLIProxyAPI."
  elif [[ -z "${configured_plugin_dir}" ]]; then
    echo "Set plugins.enabled: true and plugins.dir: ${plugin_dir} in ${config_path:-your CPA config}, then restart CLIProxyAPI."
  elif [[ "${plugin_enabled}" == "disabled" || "${plugin_enabled}" == "not set" ]]; then
    echo "Set plugins.enabled: true in ${config_path:-your CPA config}, then restart CLIProxyAPI."
  else
    echo "Restart CLIProxyAPI, then open the Phoenix dashboard."
  fi
}

if ((print_next_step)); then
  next_step
  exit 0
fi

if ((check_only || dry_run)); then
  if ((dry_run)); then
    echo "Mode: preview (no files changed)"
  else
    echo "Mode: check (no files changed)"
  fi
  echo "Artifact: ${artifact} (${artifact_state})"
  echo "CPA config: ${config_path:-not found} (${config_state})"
  echo "Plugin directory: ${plugin_dir} (${plugin_dir_source})"
  echo "CPA plugins.dir: ${configured_plugin_dir:-not set} (matches install: ${config_dir_match})"
  echo "Destination: ${destination} (${destination_state})"
  echo "CPA plugin setting: ${plugin_enabled}"
  echo "Next step: $(next_step)"
  exit 0
fi

if [[ "${artifact_state}" != "found" ]]; then
  echo "Missing ${artifact}. Run ./build.sh first or extract a release archive." >&2
  exit 1
fi

mkdir -p "${plugin_dir}"
cp -p "${artifact}" "${destination}"
destination_state="current"

echo "Installed: ${destination}"
echo "CPA config: ${config_path:-not found} (${config_state})"
echo "CPA plugin setting: ${plugin_enabled}"
echo "Next step: $(next_step)"
