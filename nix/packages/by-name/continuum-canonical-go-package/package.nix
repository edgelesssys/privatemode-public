{
  lib,
  buildGoModule,
}:
buildGoModule {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-ysaax344t00f7OnYnIKCBWsoi4Pal6T624sJh0AWZl0=";

  doCheck = false;

  proxyVendor = true;
}
