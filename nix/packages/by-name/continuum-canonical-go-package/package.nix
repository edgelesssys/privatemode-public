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

  vendorHash = "sha256-15asGOIgZy+BiJd7YOMPAbn8QPmOwWiY4kY78R1455U=";

  doCheck = false;

  proxyVendor = true;
}
