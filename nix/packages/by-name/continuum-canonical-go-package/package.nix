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

  vendorHash = "sha256-VaxJ2G63Lz5tWGC31ABahsnSLpsudEWbTs/Rf6M4g+A=";

  doCheck = false;

  proxyVendor = true;
}
