self:
{
  config,
  lib,
  pkgs,
  ...
}:

with lib;
let
  cfg = config.services.belowdeck;

  # Generate config.yaml from settings attrset
  configFile = pkgs.writeText "belowdeck-config.yaml" (builtins.toJSON cfg.settings);
in
{
  options.services.belowdeck = {
    enable = mkEnableOption "Belowdeck Stream Deck Plus daemon";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.system}.default;
      defaultText = literalExpression "belowdeck.packages.\${system}.default";
      description = "The belowdeck package to use.";
    };

    user = mkOption {
      type = types.str;
      default = "phinze";
      description = "User account that will run the daemon.";
    };

    mediaControlPath = mkOption {
      type = types.str;
      default = "/opt/homebrew/bin/media-control";
      description = "Path to the media-control binary (Homebrew-only dependency).";
    };

    settings = mkOption {
      type = types.attrs;
      default = { };
      description = ''
        Non-secret configuration written to config.yaml.
        Secrets (API keys, tokens) are stored in macOS Keychain
        via `belowdeck setup`.
      '';
      example = literalExpression ''
        {
          weather = { lat = "42.3601"; lon = "-71.0589"; };
          homeassistant = {
            server = "https://ha.example.com/";
            ring_light_entity = "light.ring_light";
            office_light_entity = "light.office";
          };
        }
      '';
    };
  };

  config = mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];

    launchd.user.agents.belowdeck = {
      path = [
        "/usr/bin"
        "/bin"
        "/usr/sbin"
        "/sbin"
        (builtins.dirOf cfg.mediaControlPath)
      ];
      serviceConfig = {
        ProgramArguments = [
          "${cfg.package}/bin/belowdeck"
        ];
        EnvironmentVariables = {
          BELOWDECK_CONFIG = "${configFile}";
        };
        KeepAlive = true;
        RunAtLoad = true;
        StandardOutPath = "/tmp/belowdeck.log";
        StandardErrorPath = "/tmp/belowdeck.log";
      };
    };
  };
}
