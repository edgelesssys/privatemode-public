#!/usr/bin/env bash

set -euo pipefail

release=false
verification=false
local=false
for arg in "$@"; do
  case "${arg}" in
  --release) release=true ;;
  --verification) verification=true ;;
  --local) local=true ;;
  *)
    echo "Unknown argument: ${arg}"
    exit 1
    ;;
  esac
done

env_file="app/web/.env"

# In release mode, we want to overwrite the .env file with the current
# environment variables to later commit it. Thus, we skip the backup
# mechanism here.
if [[ ${release} == "false" ]]; then
  env_backup="$(cat "${env_file}")"
  trap 'echo "${env_backup}" > "${env_file}"' EXIT
fi

# In verification mode, we want to use the existing .env file used
# in the release instead of generating it from the actual environment.
if [[ ${verification} == "false" ]]; then
  env | grep '^VITE_' >"${env_file}" || true
fi

if [[ ${local} == "true" ]]; then
  echo "LOCAL_BUILD=true" >>"${env_file}"
fi

nix build .#privatemode-chat -L
