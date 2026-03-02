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

  vendorHash = "sha256-rBPogEIx0iGMz0AkucttpVz7PFSouNRr0ErJ548xiw8=";

  doCheck = false;

  proxyVendor = true;
}
