{
  lib,
  buildGo124Module,
}:
buildGo124Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-SgqHwJ5kY4TuaOzc2Tj4vh/IG+gcI8n5BTo+3iAUhxM=";

  doCheck = false;

  proxyVendor = true;
}
