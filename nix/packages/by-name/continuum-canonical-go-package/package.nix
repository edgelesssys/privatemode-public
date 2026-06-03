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

  vendorHash = "sha256-x3UnHKrH0Nu8de7xkQ1QiTnueXtExXP6cZufocBhQZE=";

  doCheck = false;

  proxyVendor = true;
}
