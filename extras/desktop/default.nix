{
  mkGoProvider,
  pkgs,
  ...
}:

mkGoProvider {
  name = "desktop";
  directory = ./.;
  runtimeInputs = pkgs.lib.optionals pkgs.stdenv.hostPlatform.isLinux [
    pkgs.wl-clipboard
    pkgs.xclip
    pkgs.xsel
    pkgs.xdg-utils
  ];
}
