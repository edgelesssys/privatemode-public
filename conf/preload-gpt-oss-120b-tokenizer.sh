#!/usr/bin/env bash

expected_hash="446a9538cb6c348e3516120d7c08b09f57c36495e2acfffe59a5bf8b0cfb1a2d"
tokenizer_dir="/vllm-workspace/tokenizers"
tokenizer_file="o200k_base.tiktoken"
tokenizer_url="https://openaipublic.blob.core.windows.net/encodings/${tokenizer_file}"

echo "Downloading tokenizer file from $tokenizer_url to ${tokenizer_dir}/${tokenizer_file}"
mkdir -p "$tokenizer_dir"
curl --fail --silent --show-error --location \
  --connect-timeout 10 --max-time 340 \
  --retry 5 --retry-delay 2 --retry-all-errors \
  "$tokenizer_url" -o "${tokenizer_dir}/${tokenizer_file}"

if [ "$(sha256sum ${tokenizer_dir}/${tokenizer_file} | cut -d ' ' -f 1)" != "${expected_hash}" ]; then
  echo ${tokenizer_file} hash mismatch: "$(sha256sum ${tokenizer_dir}/${tokenizer_file} | cut -d ' ' -f 1)"
  exit 1
fi

echo "Tokenizer file downloaded successfully."

export TIKTOKEN_ENCODINGS_BASE="$tokenizer_dir"
