{
  lib,
  buildGo126Module,
}:
buildGo126Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-llrxSyhS7rOGctkxd1fC91jBd7T3yoyxajsAx1YAgcc=";

  doCheck = false;

  proxyVendor = true;
}
