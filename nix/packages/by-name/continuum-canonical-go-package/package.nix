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

  vendorHash = "sha256-WC8aoSrzHzu6a2KNkLEOKnN56QLQ7iCfqp6ZXd4haok=";

  doCheck = false;

  proxyVendor = true;
}
