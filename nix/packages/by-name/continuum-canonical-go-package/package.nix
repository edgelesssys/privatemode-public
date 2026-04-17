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

  vendorHash = "sha256-dS9o+AK6PNHIUhINfy+FoqSqMo0qoAum2T7big8z8JA=";

  doCheck = false;

  proxyVendor = true;
}
