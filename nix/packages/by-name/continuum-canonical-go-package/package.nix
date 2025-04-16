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

  vendorHash = "sha256-4yPTaQKVLKOPQOD/hd0dJbiY4vp0HKtqomG11QJZk3E=";

  doCheck = false;

  proxyVendor = true;
}
