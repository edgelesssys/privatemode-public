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

  vendorHash = "sha256-PomvHU2qdyTIfu8s8bBOnFZxiRLr/EBuPLivaOBDspU=";

  doCheck = false;

  proxyVendor = true;
}
