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

  vendorHash = "sha256-joNCdYXDpQWDMBr9kWz84gGf8GrTnK6ij8GViln8pCs=";

  doCheck = false;

  proxyVendor = true;
}
