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

  vendorHash = "sha256-Q38Xa/yITVBUiT5BjEfRdOBZqs9ictt9CICd8tOY2DY=";

  doCheck = false;

  proxyVendor = true;
}
