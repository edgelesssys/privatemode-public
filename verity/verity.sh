#!/usr/bin/env bash

set -exo pipefail

device="${1}"
model_src="${2}"
commit="${3}"

if [[ -z ${device} ]] || [[ -z ${model_src} ]] || [[ -z ${commit} ]]; then
  echo "Usage: $0 <device> <model_source_url> <commit_hash>"
  exit 1
fi

# Verify needed tools are installed
required_tools=("git" "git-lfs" "systemd-repart" "sgdisk" "mount")
for tool in "${required_tools[@]}"; do
  if ! command -v "$tool" &>/dev/null; then
    echo "$tool is not installed"
    exit 1
  fi
done

git_dns_name=$(echo "${model_src}" | cut -d/ -f3)
echo "machine ${git_dns_name} login api password ${GIT_PAT}" >"${HOME}/.netrc"

git lfs install

# Ensure we have access to everything under /dev
mount -t devtmpfs none /dev

TMPDIR="${TMPDIR:-/tmp}"
export TMPDIR
mkdir -p "${TMPDIR}"
tmp_repo=$(mktemp -d)

GIT_LFS_SKIP_SMUDGE=1 git clone --depth 1 "${model_src}" "${tmp_repo}"
pushd "${tmp_repo}" || exit 1
git checkout "${commit}"
git lfs pull
find . -exec touch -d @0 {} +
popd || exit 1

excluded_files=()
if [[ -n ${EXCLUDE_GIT_FILES} ]]; then
  readarray -t excluded_files <<<"$(echo "${EXCLUDE_GIT_FILES}" | tr ',' '\n')"
fi
extra_exclude_directives=()
for file in "${excluded_files[@]}"; do
  file_glob="${tmp_repo}/${file}"
  shopt -s nullglob
  # shellcheck disable=SC2206 # We want globbing and splitting
  absolute_paths=(${file_glob})
  shopt -u nullglob
  extra_exclude_directives+=("ExcludeFiles=${absolute_paths[*]}\n")
done

sed -e "s|@@SOURCE_REPO@@|${tmp_repo}|g" \
  -e "s|@@EXTRA_EXCLUDED_FILES@@|$(printf '%s' "${extra_exclude_directives[@]}")|g" \
  /repart/00-repart.conf.template >/repart/00-repart.conf

ls /repart
for file in /repart/*.conf; do
  echo "Processing ${file}"
  cat "${file}"
done

constant_uuid="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
sgdisk --zap-all "${device}"
sgdisk --disk-guid=${constant_uuid} "${device}"

SOURCE_DATE_EPOCH=0 systemd-repart --definitions=/repart/ --seed="${constant_uuid}" "${device}" --dry-run=no --json=pretty | tee repart.json
