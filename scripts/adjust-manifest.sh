#!/usr/bin/env bash
set -euo pipefail

manifest=$1

# The machines on Scaleway are Genoa machines. Coordinator validation fails if we have Milan reference values in the manifest.
yq eval -i 'del(.ReferenceValues.snp[] | select(.ProductName == "Milan"))' "$manifest"

yq eval -i '.ReferenceValues.snp[].MinimumTCB.BootloaderVersion=9' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.TEEVersion=0' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.SNPVersion=21' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.MicrocodeVersion=72' "$manifest"

# add required SAN for secret-service mesh cert.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [env(SECRET_SERVICE_K8S_DOMAIN)]' "$manifest"
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [env(SECRET_SERVICE_DOMAIN)]' "$manifest"

# always accepts the production URL. This is required so the manifest set during staging is still valid for production.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += ["secret.privatemode.ai"]' "$manifest"

# remove workload owner key because we don't use the functionality and it makes the trust story clearer
yq eval -i 'del(.WorkloadOwnerKeyDigests)' "$manifest"
