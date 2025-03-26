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

  vendorHash = "sha256-bvD1yzsDrB3OQJg2vrrarQXCjVCC2pfaYS8mA9Sp1jA=";

  doCheck = false;

  proxyVendor = true;
}
