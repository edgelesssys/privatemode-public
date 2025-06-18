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

  vendorHash = "sha256-kn2RCoOXHJvJmfAcP+a1oIOVMFurOcLRQ763c6FWvb8=";

  doCheck = false;

  proxyVendor = true;
}
