#!/usr/bin/env bash

set -eo pipefail

model_src="${1}"
commit="${2}"

if [[ -z ${model_src} ]] || [[ -z ${commit} ]]; then
  echo "Usage: $0 <model_source_url> <commit_hash>"
  exit 1
fi

if [[ -n ${GIT_PAT} ]]; then
  git_dns_name=$(echo "${model_src}" | cut -d/ -f3)
  echo "machine ${git_dns_name} login api password ${GIT_PAT}" >"${HOME}/.netrc"
else
  echo "Warning: no git access token set, repository cloning may fail or be subject to rate limits"
fi

git lfs install

excluded_files=()
if [[ -n ${EXCLUDE_GIT_FILES} ]]; then
  readarray -t excluded_files <<<"$(echo "${EXCLUDE_GIT_FILES}" | tr ',' '\n')"
fi

mkdir -p "${TMPDIR:-/tmp}"
cd "${TMPDIR:-/tmp}" || exit 1

umask 22
repo_name="model_repo"
GIT_LFS_SKIP_SMUDGE=1 git clone "${model_src}" "${repo_name}"
pushd "${repo_name}" || exit 1
git checkout "${commit}"

# Configure git-lfs to exclude specific files if they follow patterns
if [[ ${#excluded_files[@]} -gt 0 ]]; then
  # Convert excluded files to git-lfs exclude patterns
  exclude_patterns=()
  for file in "${excluded_files[@]}"; do
    exclude_patterns+=("${file}")
  done
  # Set git-lfs to exclude these patterns
  git config lfs.fetchexclude "$(
    IFS=','
    echo "${exclude_patterns[*]}"
  )"
fi

git lfs pull
rm -r .git
shopt -s nullglob
# shellcheck disable=SC2068 # We want globbing and splitting
rm -rf ${excluded_files[@]}
shopt -u nullglob

popd || exit 1

staging_path="${TMPDIR:-/tmp}"
if [[ -n ${STAGING_PATH} ]]; then
  staging_path="${STAGING_PATH}"
fi
mkdir -p "${staging_path}"

model_name="$(normalize-model-name.sh "${model_src}")"
temp_disk="${staging_path}/${model_name}.tmp"

mkfs.erofs -T0 -x-1 --all-root -U00000000-0000-0000-0000-000000000000 "${temp_disk}" "${repo_name}"
hash_offset=$(stat -c%s "${temp_disk}")
verity_output=$(veritysetup format --uuid=00000000-0000-0000-0000-000000000000 --salt=00 --hash-offset="${hash_offset}" "${temp_disk}" "${temp_disk}")
echo "${verity_output}"
root_hash=$(echo "${verity_output}" | awk '/Root hash:/ {print $NF}')

disk_name="${staging_path}/${model_name}/${root_hash}"
mkdir -p "${staging_path}/${model_name}"
mv "${temp_disk}" "${disk_name}"

file_hash=$(sha256sum "${disk_name}" | awk '{print $1}')

cat <<EOF >"${disk_name}.json"
{
  "model_source": "${model_src}",
  "commit_hash": "${commit}",
  "excluded_files": "${EXCLUDE_GIT_FILES}",
  "root_hash": "${root_hash}",
  "hash_offset": "${hash_offset}",
  "erofs_version": "$(mkfs.erofs --version | head -n1 | awk '{print $NF}')",
  "veritysetup_version": "$(veritysetup --version | awk '{print $2}')",
  "file_hash_sha256": "${file_hash}"
}
EOF
