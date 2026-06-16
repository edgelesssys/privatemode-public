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

  vendorHash = "sha256-fqCFmEA9nJBEJif11Z497wKuLPYBee13ffcm4H6ByW0=";

  doCheck = false;

  proxyVendor = true;
}
