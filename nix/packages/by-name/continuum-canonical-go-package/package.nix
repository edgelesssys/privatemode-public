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

  vendorHash = "sha256-i4sQDqlG6NlCMX0y+WFpcd4Zvdm56oGO4iHIzp0q8DQ=";

  doCheck = false;

  proxyVendor = true;
}
