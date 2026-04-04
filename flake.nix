{
  description = "Monotool - CLI tool for building and deploying containerized applications";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          monotool = pkgs.buildGoModule {
            pname = "monotool";
            version = self.shortRev or self.dirtyShortRev or "dev";
            src = ./.;
            vendorHash = "sha256-g5J9ktcgaD0qEYr9OPCvEZT2G8TIQnjQWCafBb3ccjA=";
          };
          default = self.packages.${system}.monotool;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            docker
            git
          ];

          shellHook = ''
            echo "Monotool development environment"
            echo "Go version: $(go version)"
          '';
        };
      }
    ) // {
      overlays.default = final: prev: {
        monotool = self.packages.${prev.system}.monotool;
      };
    };
}
