{ pkgs, version, ... }:

pkgs.buildGoModule {
  pname = "nixpkgs-update-notifier";
  inherit version;
  # In 'nix develop', we don't need a copy of the source tree
  # in the Nix store.
  src = ./.;

  vendorHash = "sha256-jclP3ZgEe3xLDqNvQFs3tZIwtN3Mj4lumvG9lQVWb4Y=";
}
