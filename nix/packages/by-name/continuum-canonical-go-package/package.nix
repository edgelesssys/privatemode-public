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

  vendorHash = "sha256-adHo+dzpeWVnWk3VDVohZJK4C080JJRe/9XqaieMkuI=";

  doCheck = false;

  proxyVendor = true;
}
