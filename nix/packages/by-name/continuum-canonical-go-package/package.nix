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

  vendorHash = "sha256-8GD8dKScTAqXKZ1LrTppGjG1cE6d0q1i3yWwLtb3WEA=";

  doCheck = false;

  proxyVendor = true;
}
