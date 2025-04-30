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

  vendorHash = "sha256-4gOHPKN01OgXNYoPbwqGQv42PUMnQ069ftMHvqJK1oc=";

  doCheck = false;

  proxyVendor = true;
}
