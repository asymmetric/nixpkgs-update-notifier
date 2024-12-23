{
  description = "nixpkgs-update-notifier flake";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";

  outputs = { self, nixpkgs }:
    let

      # to work with older version of flakes
      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";

      # Generate a user-friendly version number.
      version = builtins.substring 0 8 lastModifiedDate;

      # System types to support.
      supportedSystems = [ "x86_64-linux" "aarch64-linux" ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

    in
    {

      # Provide some binary packages for selected system types.
      packages = forAllSystems (system: rec {
        nixpkgs-update-notifier = nixpkgsFor.${system}.callPackage ./package.nix { inherit version; };

        default = nixpkgs-update-notifier;
      });

      # Add dependencies that are only needed for development
      devShells = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              go-tools # staticcheck
              gopls
              gotools
              sqlc
            ];
          };
        });

      overlays.default = final: prev: {
        nixpkgs-update-notifier = self.packages.${final.system}.nixpkgs-update-notifier;
      };

      nixosModules.default = { config, lib, pkgs, ... }: {
        imports = [
          ./module.nix
        ];
        nixpkgs.overlays = [ self.overlays.default ];
      };
    };
}
