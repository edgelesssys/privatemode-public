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

  vendorHash = "sha256-gXim7E+VTfV3CeZ4pU544MYUMJ6MJ9UWpjacEuWbc1M=";

  doCheck = false;

  proxyVendor = true;
}
