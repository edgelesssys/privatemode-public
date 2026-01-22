#!/usr/bin/env nix
#! nix shell nixpkgs#yq-go -c bash
# shellcheck shell=bash

set -euo pipefail

manifest=$1

# set TCB versions
yq eval -i '.ReferenceValues.snp[].MinimumTCB.BootloaderVersion=10' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.TEEVersion=0' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.SNPVersion=28' "$manifest"
yq eval -i '.ReferenceValues.snp[].MinimumTCB.MicrocodeVersion=88' "$manifest"

# configure GuestPolicy and PlatformInfo
yq eval -i '.ReferenceValues.snp[].GuestPolicy={ "SMT":true, "MigrateMA":false, "Debug":false, "CXLAllowed":false, "PageSwapDisable":true }' "$manifest"
yq eval -i '.ReferenceValues.snp[].PlatformInfo={ "SMTEnabled":false, "ECCEnabled":true, "AliasCheckComplete":true }' "$manifest"

# add chip ids
yq eval -i '.ReferenceValues.snp[].AllowedChipIDs=[
"06B3977C15C8E92E9897EBC3A0BE1E6EF6E2E70F9B57A327854F8E735A608EC1E528821C2FA770E0DD175CFD421E492C45A164457C4C669674B695368C08D36D",
"11495177E7796396BC5E4B8F239727AE12D8E8B0A7C2D3866D14ADC385D931DB6EC9AC27810CF8B8593E6C278C16DEA882036ABBF6820BC4EBF10E9A8F44FBF9",
"1886AFB73CF8656295B87CC64457DF576C43F95F00B713B7F79F88239582CF423EFD9023E211AFEC216696078D30F39D13149699F184B4B30411F91FAF3F0D86",
"5050718946CB028EAB9D7BB648468B4807F25229C3EE639B70F1D80251EF19CC18D3E47A991178A1A841112461D297E522F4E9C166EB20E612AB35F60E29FB1C",
"8DCEB46E7C5645F2B347D167616CE387CA071E37F04B909186A0C93A26DFBC50B3FA20F08DF69FEF42722A6BF42BF25B39E660754E899410D917EED08142CE53"
]' "$manifest"

# add required SAN for secret-service mesh cert.
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [env(SECRET_SERVICE_K8S_INTERNAL_DOMAIN)]' "$manifest"
yq eval -i '(.Policies[] | select(.WorkloadSecretID | contains("secret-service")).SANs) += [strenv(SECRET_SERVICE_K8S_HEADLESS_DOMAIN)]' "$manifest"
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
