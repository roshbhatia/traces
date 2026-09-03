{
  mkShellProvider,
  pkgs,
  ...
}:

mkShellProvider {
  name = "git";
  directory = ./.;
  runtimeInputs = [
    pkgs.bash
    pkgs.gitMinimal
  ];
}
