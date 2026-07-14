# home-manager module for nfu.
#
# Usage in your home-manager config:
#
#   imports = [ inputs.nfu.homeManagerModules.default ];
#   programs.nfu = {
#     enable = true;
#     settings = {
#       flakeRoot = "~/Projects/nixos-config";
#       commit = false;   # set true to auto-commit flake.lock after updates
#     };
#   };
#
# Then `nfu totality` updates the totality input in that flake from anywhere.
self:
{ config, lib, pkgs, ... }:
let
  cfg = config.programs.nfu;
  tomlFormat = pkgs.formats.toml { };
in
{
  options.programs.nfu = {
    enable = lib.mkEnableOption "nfu, a small flake-input updater";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.system}.nfu;
      defaultText = lib.literalExpression "nfu.packages.\${system}.nfu";
      description = "The nfu package to install.";
    };

    settings = lib.mkOption {
      type = tomlFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          flakeRoot = "~/Projects/nixos-config";
          commit = false;
        }
      '';
      description = ''
        nfu configuration, written to ~/.config/nfu/config.toml.
        See config.example.toml in the nfu repo for the schema.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    home.packages = [ cfg.package ];
    xdg.configFile = lib.mkIf (cfg.settings != { }) {
      "nfu/config.toml".source = tomlFormat.generate "nfu-config.toml" cfg.settings;
    };
  };
}
