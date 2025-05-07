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

  vendorHash = "sha256-6krYRe2d2GmhjUQM8//qkewjMOtHlf6rkHWlJRuoNSA=";

  doCheck = false;

  proxyVendor = true;
}
