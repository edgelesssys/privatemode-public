{
  inputs = {
    nixpkgs = {
      url = "github:NixOS/nixpkgs/nixos-unstable";
    };
    flake-utils = {
      url = "github:numtide/flake-utils";
    };
    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
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
          config.allowUnfree = true;

          overlays = [
            (_final: prev: (import ./nix/packages { inherit (prev) lib callPackage; }))
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
        } // pkgs;

        devShells = {
          default = pkgs.callPackage ./nix/devShells/devshell.nix { };

          ci = pkgs.callPackage ./nix/devShells/ci-shell.nix { };

          ci-remote-builder = pkgs.callPackage ./nix/devShells/ci-remote-builder-shell.nix { };
        };

        formatter = treefmtEval.config.build.wrapper;

        checks = {
          formatting = treefmtEval.config.build.check self;
        };
      }
    );
}
