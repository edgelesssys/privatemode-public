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

  vendorHash = "sha256-/F7iUIClYQaAmH6RAOz2YumdRvY+u1aGa8BfYQmQWMc=";

  doCheck = false;

  proxyVendor = true;
}
