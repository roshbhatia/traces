{
  mkGoProvider,
  pkgs,
  ...
}:

mkGoProvider {
  name = "git";
  directory = ./.;
  runtimeInputs = [
    pkgs.gitMinimal
  ];
}
