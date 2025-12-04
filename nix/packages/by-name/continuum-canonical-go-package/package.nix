{
  lib,
  buildGo125Module,
}:
buildGo125Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-erDyZBeoMocQAWZWbUDohTXspAFCYqQt/2jCxkIbKhc=";

  doCheck = false;

  proxyVendor = true;
}
