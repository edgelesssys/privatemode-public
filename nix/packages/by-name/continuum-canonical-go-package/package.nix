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

  vendorHash = "sha256-wb6kr92E67zeyZRJCVSLnc5jafIum7swrBUC6Iz2TTk=";

  doCheck = false;

  proxyVendor = true;
}
