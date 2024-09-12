{ config, lib, pkgs, ... }: {
  options = {
    services.nixpkgs-update-notifier = {
      enable = lib.mkEnableOption "nixpkgs-update-notifier";
    };
  };
  config =
    let cfg = config.services.nixpkgs-update-notifier;
    in lib.mkIf cfg.enable {
      systemd.services.nixpkgs-update-notifier = {
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];
        serviceConfig = {
          Restart = "on-failure";
          EnvironmentFile = cfg.passwordFile;
          ExecStart = toString [
            (lib.getExe pkgs.nixpkgs-update-notifier)
            "-matrix.username ${cfg.username}"
            "-db ${cfg.dataDir}/data.db"
            (lib.optionalString (cfg.ticker != null) "-ticker ${cfg.ticker}")
            (lib.optionalString cfg.debug "-debug")
          ];
          StateDirectory = "nixpkgs-update-notifier";
        };
      };
    };
}
