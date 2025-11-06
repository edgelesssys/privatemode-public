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

  vendorHash = "sha256-/yu86mwLRKdH8tpupt418Rmx5kn0j/0zaY1lPlHAlcA=";

  doCheck = false;

  proxyVendor = true;
}
