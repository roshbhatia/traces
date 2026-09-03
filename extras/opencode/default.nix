{
  mkGoProvider,
  pkgs,
  ...
}:

mkGoProvider {
  name = "opencode";
  directory = ./.;
  runtimeInputs = [ pkgs.opencode ];
}
