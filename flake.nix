{
  description = "GoPengAI — Agent System development environment";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; };
    in {
      devShells.${system}.default = pkgs.mkShell {
        name = "gopengai-dev";

        packages = with pkgs; [
          go
          gopls
          sqlc
          goose
          gnumake
          git
        ];

        shellHook = ''
          echo "🚀 GoPengAI dev shell loaded"
          echo "  Go:     $(go version)"
          echo "  sqlc:   $(sqlc version)"
          echo "  goose:  $(goose version)"
        '';
      };
    };
}
