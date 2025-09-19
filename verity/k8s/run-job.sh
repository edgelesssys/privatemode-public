#!/usr/bin/env bash

set -e

if [[ -z ${DISK_SIZE_GB} ]] || [[ -z ${MODEL_SOURCE} ]] || [[ -z ${COMMIT_HASH} ]]; then
  echo "Usage: DISK_SIZE_GB=<disk_size_in_GiB> MODEL_SOURCE=<source_url> COMMIT_HASH=<commit_hash> [GIT_PAT=<git_pat>] [EXCLUDED_MODEL_REPO_FILES=file1,file2] $0"
  exit 1
fi

script_dir="$(dirname "$(readlink -f "$0")")"

# Generate a 6 character random lower-case string to use as the snapshot and pvc name
# shellcheck disable=SC2018 # we explicitly only want lower case a-z to conform to k8s naming rules
pvc_name_suffix="$(head /dev/urandom | tr -dc 'a-z' | fold -w 6 | head -n 1)"
job_name="job/verity-disk-generator-${pvc_name_suffix}"

sed -e "s|MODEL_SOURCE|${MODEL_SOURCE}|g" \
  -e "s|COMMIT_HASH|${COMMIT_HASH}|g" \
  -e "s|GIT_PAT_BASE64|$(echo -n "${GIT_PAT}" | base64 -w0)|g" \
  -e "s/NAME_SUFFIX/${pvc_name_suffix}/g" \
  -e "s/EXCLUDED_MODEL_REPO_FILES/${EXCLUDED_MODEL_REPO_FILES}/g" \
  "${script_dir}/job.yaml.template" >/tmp/job.yaml

sed -e "s/DISK_SIZE_GB/${DISK_SIZE_GB}/g" \
  -e "s/NAME_SUFFIX/${pvc_name_suffix}/g" \
  "${script_dir}/pvc.yaml.template" >/tmp/pvc.yaml

set -x

kubectl apply -f /tmp/pvc.yaml
kubectl apply -f /tmp/job.yaml

kubectl wait --for=condition=complete --timeout=-1s "${job_name}" &
completion_pid=$!

kubectl wait --for=condition=failed --timeout=-1s "${job_name}" &
failure_pid=$!

if wait -n "${completion_pid}" "${failure_pid}"; then
  if ! kill -0 "${completion_pid}" 2>/dev/null; then
    echo "Job completed successfully"
    kill "${failure_pid}"
  elif ! kill -0 "${failure_pid}" 2>/dev/null; then
    echo "Job failed"
    kill "${completion_pid}"
    exit 1
  fi
fi

verity_hash=$(kubectl logs "${job_name}" | awk '/All done\./{found=1; next} found' | jq -r '.[0].roothash')
storage_class_name="$("${script_dir}/generate-storage-class-name.sh" "${MODEL_SOURCE}")"
pv_name="$(kubectl get -f /tmp/pvc.yaml -o jsonpath='{.spec.volumeName}')"

sed -e "s|NAME_SUFFIX|${pvc_name_suffix}|g" \
  -e "s|PVC_NAME|${pv_name}|g" \
  "${script_dir}/backing_image.yaml.template" >/tmp/backing_image.yaml
kubectl apply -f /tmp/backing_image.yaml

# avoid spamming stdout with the Pod spec
set +x

echo "Waiting for BackingImage creation..."
backing_image_checksum=""
while true; do
  backing_image_status=$(kubectl get -f /tmp/backing_image.yaml -o yaml || true)
  if [[ -z ${backing_image_status} ]]; then
    echo "BackingImage not created"
    continue
  fi

  backing_image_checksum=$(yq '.status.checksum' <<<"${backing_image_status}")
  min_copies=$(yq '.spec.minNumberOfCopies' <<<"${backing_image_status}")

  disk_file_status=$(yq '.status.diskFileStatusMap[].state' <<<"${backing_image_status}")

  ready_copies=$(grep -c "ready" <<<"${disk_file_status}" || true)
  if [[ ${ready_copies} -ge ${min_copies} ]]; then
    break
  fi

  echo "BackingImage not ready: ${ready_copies}/${min_copies} copies are fully replicated."
  sleep 5
done

set -x

current_version=$(just print-version)
build_version=${current_version%%-pre*}

sed \
  -e "s|VERITY_HASH|${verity_hash}|g" \
  -e "s|DISK_SIZE_GB|${DISK_SIZE_GB}|g" \
  -e "s|MODEL_SOURCE|${MODEL_SOURCE}|g" \
  -e "s|COMMIT_HASH|${COMMIT_HASH}|g" \
  -e "s|EXCLUDED_FILES|${EXCLUDED_MODEL_REPO_FILES}|g" \
  -e "s|NAME_SUFFIX|${pvc_name_suffix}|g" \
  -e "s|BACKING_IMAGE_CHECKSUM|${backing_image_checksum}|g" \
  -e "s|STORAGE_CLASS_NAME|${storage_class_name}|g" \
  -e "s|BUILD_VERSION|${build_version}|g" \
  "${script_dir}/storageclass.yaml.template" >/tmp/storageclass.yaml

kubectl apply -f /tmp/storageclass.yaml

# Delete the job and PVC
kubectl delete -f /tmp/job.yaml
kubectl delete -f /tmp/pvc.yaml

echo "StorageClass ${storage_class_name} from ${MODEL_SOURCE} created successfully"
echo "${verity_hash}" >/tmp/verity_root_hash
