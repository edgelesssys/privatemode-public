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

  vendorHash = "sha256-yGkO3VI1pZwSOMsmTUsUCz6m9oZW/2y4qJ/rLesj23c=";

  doCheck = false;

  proxyVendor = true;
}
