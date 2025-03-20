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

  vendorHash = "sha256-HvfFEr/2b6EgvrKC9IxctOdBaRqpR19n53vmxjPnL+U=";

  doCheck = false;

  proxyVendor = true;
}
