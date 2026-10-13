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

  vendorHash = "sha256-AteBohqLXWc+YL+rTkNtv6bz9DY38G22MmH5aDRQDXg=";

  doCheck = false;

  proxyVendor = true;
}
