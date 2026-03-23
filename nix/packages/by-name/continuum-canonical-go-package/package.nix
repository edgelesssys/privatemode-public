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

  vendorHash = "sha256-gPm6p1jwCkFS/ZXpxs3xOt8BUXtYUjmt+h/Pn4pevZU=";

  doCheck = false;

  proxyVendor = true;
}
