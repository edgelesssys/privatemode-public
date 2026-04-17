#!/usr/bin/env bash

model_source=${1}

# parse model source to valid kubernetes resource name
# start by stripping the protocol and host
model_source_cleaned=${model_source#*//}
model_source_cleaned=${model_source_cleaned#*/}
# strip potential trailing '.git' suffix
model_source_cleaned=${model_source_cleaned%.git}
# replace all non-alphanumeric characters with a hyphen
model_source_cleaned=$(echo -n "${model_source_cleaned}" | tr -c '[:alnum:]' '-')
# replace all multiple hyphens with a single hyphen
model_source_cleaned=$(echo "${model_source_cleaned}" | tr -s '-')
# convert to lowercase
model_name=$(echo "${model_source_cleaned}" | tr '[:upper:]' '[:lower:]')

echo "${model_name}"
