{
  description = "GoFHIR - Monorepo for medical data gateway";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        go = pkgs.go_1_26;
      in {
        devShells.default = pkgs.mkShell {
          buildInputs = [
            go
            pkgs.gopls
            pkgs.golangci-lint
            pkgs.go-tools
            pkgs.sqlite
            pkgs.reflex
          ];

          shellHook = ''
            export CGO_ENABLED=0
            echo "GoFHIR dev environment loaded"
            echo "Go version: $(go version)"
          '';
        };
      }
    );
}