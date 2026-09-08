{
  mkGoProvider,
  pkgs,
  ...
}:

mkGoProvider {
  name = "devin";
  directory = ./.;
  runtimeInputs = [ pkgs.sqlite ];
}
