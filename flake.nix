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
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
            delve
            docker
            git
          ];

          shellHook = ''
            echo "Monotool development environment"
            echo "Go version: $(go version)"
          '';
        };
      }
    );
}
