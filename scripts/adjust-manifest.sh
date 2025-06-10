#!/usr/bin/env nix
#! nix shell nixpkgs#yq-go -c bash
# shellcheck shell=bash

set -euo pipefail

manifest=$1

# The machines on Scaleway are Genoa machines. Coordinator validation fails if we have Milan reference values in the manifest.
yq eval -i 'del(.ReferenceValues.snp[] | select(.ProductName == "Milan"))' "$manifest"

# set TCB versions
yq eval -i '.ReferenceValues.snp[].MinimumTCB.BootloaderVersion=10' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.TEEVersion=0' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.SNPVersion=23' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.MicrocodeVersion=84' "$manifest"

# configure GuestPolicy and PlatformInfo
yq eval -i '.ReferenceValues.snp[].GuestPolicy={ "SMT":true, "MigrateMA":false, "Debug":false, "CXLAllowed":false }' "$manifest"
yq eval -i '.ReferenceValues.snp[].PlatformInfo={ "SMTEnabled":true, "AliasCheckComplete":true }' "$manifest"

# add required SAN for secret-service mesh cert.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [env(SECRET_SERVICE_K8S_DOMAIN)]' "$manifest"
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [env(SECRET_SERVICE_DOMAIN)]' "$manifest"

# always accepts the production URL. This is required so the manifest set during staging is still valid for production.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += ["secret.privatemode.ai"]' "$manifest"

# SAN[0] is used as the Common Name of the certificate
# The secret-service acts as our etcd root user, therefore requires root as the certs CN.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) |= ["root"] + .' "$manifest"

# Workloads and Unstructured API act as etcd clients, therefore require the name of a registered etcd user as the certs CN.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("workload-")).SANs) |= ["continuum-etcd-client"] + .' "$manifest"
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("unstructured")).SANs) |= ["continuum-etcd-client"] + .' "$manifest"

# remove workload owner key because we don't use the functionality and it makes the trust story clearer
yq eval -i 'del(.WorkloadOwnerKeyDigests)' "$manifest"
