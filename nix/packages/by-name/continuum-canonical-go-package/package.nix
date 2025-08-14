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

  vendorHash = "sha256-EfO+YyZxeQpsEJh4Sg0sop64W9/2j9v0iAJDo2JCUls=";

  doCheck = false;

  proxyVendor = true;
}
