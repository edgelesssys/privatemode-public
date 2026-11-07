{
  lib,
  buildGo127Module,
}:
buildGo127Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-39XdybYi2Mk894feBilLFBsudgKJp8reSp1Kb16liBU=";

  doCheck = false;

  proxyVendor = true;
}
