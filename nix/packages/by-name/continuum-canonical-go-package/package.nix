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

  vendorHash = "sha256-kEilhR0Eru8ntH0yNLEfA16gB5AMvw8xj7jDxFidWqM=";

  doCheck = false;

  proxyVendor = true;
}
