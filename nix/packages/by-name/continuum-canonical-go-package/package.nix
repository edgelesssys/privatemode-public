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

  vendorHash = "sha256-sRBkcqrc/IwdA8fs11OVDDWAcA9Gtzfs3j7QdW7GMKI=";

  doCheck = false;

  proxyVendor = true;
}
