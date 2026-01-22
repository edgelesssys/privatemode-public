{
  lib,
  buildGo125Module,
}:
buildGo125Module {
  pname = "continuum-canonical-go-package";
  version = lib.continuumVersion;

  src = lib.continuumRepoRootSrc [
    "go.mod"
    "go.sum"
  ];

  vendorHash = "sha256-JIuG1Jrq9jlRsrudAjaZMVdMaL6iNbOZQXti/B/l1VA=";

  doCheck = false;

  proxyVendor = true;
}
