# A 'wrapped' Go builder for Continuum, which doesn't require a `vendorHash` to be set in each package.
# Instead, one central vendor hash is set here, and all packages inherit it.

{
  buildGoModule,
  continuum-canonical-go-package,
}:
args:
(buildGoModule (
  {
    # We run tests in CI, so don't run them at build time.
    doCheck = false;

    # Disable CGO by default.
    env.CGO_ENABLED = "0";
  }
  // args
)).overrideAttrs
  (_oldAttrs: {
    inherit (continuum-canonical-go-package)
      goModules
      vendorHash
      proxyVendor
      deleteVendor
      ;
  })
