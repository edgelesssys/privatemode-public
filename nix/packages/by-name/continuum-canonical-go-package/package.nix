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

  vendorHash = "sha256-eagA3VZNl1SJy6kS2yLfHJB/Fv7GE12TIEoxlSmIDAg=";

  doCheck = false;

  proxyVendor = true;
}
