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

  vendorHash = "sha256-zHhCjmsFOb8bX4NG588gvi1Lyw/RPItrqqhUblbQkms=";

  doCheck = false;

  proxyVendor = true;
}
