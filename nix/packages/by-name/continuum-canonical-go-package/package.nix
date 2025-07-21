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

  vendorHash = "sha256-+yQTIMX8CG+cDLXlChbB4nOObWZMI975dOXQkOiFtOo=";

  doCheck = false;

  proxyVendor = true;
}
