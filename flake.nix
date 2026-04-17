{
  inputs = {
    nixpkgs = {
      url = "github:NixOS/nixpkgs/nixos-unstable";
    };
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    flake-utils.url = "github:numtide/flake-utils";
  };

  nixConfig = {
    extra-substituters = [ "https://edgelesssys.cachix.org" ];
    extra-trusted-public-keys = [
      "edgelesssys.cachix.org-1:erQG/S1DxpvJ4zuEFvjWLx/4vujoKxAJke6lK2tWeB0="
    ];
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      treefmt-nix,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
            permittedInsecurePackages = [
              "electron-38.8.4" # TODO(msanft): Remove once native app is removed.
            ];
          };
          overlays = [
            # Cross-platform packages that may also be built for and run on MacOS.
            # Examples: Dev tools, Client-side software such as the PM proxy.
            (_final: prev: (import ./nix/packages { inherit (prev) lib callPackage; }))
            # Linux-specific packages. These will always be built for and run on Linux.
            # If no Linux (remote) builder is available, these packages will fail to build on MacOS.
            # Examples: Server-side software such as the API gateway.
            (_final: _prev: { linuxPkgs = self.legacyPackages.x86_64-linux; })
            # Custom Nix library functions and utilities.
            (_final: prev: { lib = prev.lib // (import ./nix/lib { inherit (prev) lib callPackage; }); })
          ];
        };

        treefmtEval = treefmt-nix.lib.evalModule pkgs ./nix/treefmt.nix;
      in
      {
        # Use `legacyPackages` instead of `packages` for the reason explained here:
        # https://github.com/NixOS/nixpkgs/blob/34def00657d7c45c51b0762eb5f5309689a909a5/flake.nix#L138-L156
        # Note that it's *not* a legacy attribute.
        legacyPackages = {
          generate = pkgs.callPackage ./nix/generate.nix { };
        }
        // pkgs;

        devShells = {
          default = pkgs.callPackage ./nix/devShells/devshell.nix { };
          ci = pkgs.callPackage ./nix/devShells/ci-shell.nix { };
        };

        formatter = treefmtEval.config.build.wrapper;

        checks = {
          formatting = treefmtEval.config.build.check self;
        };
      }
    );
}
