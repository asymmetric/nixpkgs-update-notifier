{
  config,
  lib,
  pkgs,
  ...
}:
{
  options = {
    services.nixpkgs-update-notifier = {
      enable = lib.mkEnableOption "nixpkgs-update-notifier";
      username = lib.mkOption { type = lib.types.str; };
      passwordFile = lib.mkOption { type = lib.types.path; };
      dataDir = lib.mkOption {
        type = lib.types.path;
        default = "/var/lib/nixpkgs-update-notifier";
      };
      ticker = lib.mkOption {
        type = lib.types.nullOr lib.types.str;
        default = null;
      };
      debug = lib.mkOption {
        type = lib.types.bool;
        default = false;
      };
    };
  };

  config =
    let
      cfg = config.services.nixpkgs-update-notifier;
    in
    lib.mkIf cfg.enable {
      systemd.services.nixpkgs-update-notifier = {
        wantedBy = [ "multi-user.target" ];
        after = [ "network.target" ];
        startLimitIntervalSec = 0;
        serviceConfig = {
          Restart = "on-failure";
          RestartPreventExitStatus = [ 100 ];
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
