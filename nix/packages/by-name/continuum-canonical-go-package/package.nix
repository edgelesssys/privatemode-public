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

  vendorHash = "sha256-vBeUKh98r2svZny78u3Mra60JRv7od2bP0bakB5y8XM=";

  doCheck = false;

  proxyVendor = true;
}
