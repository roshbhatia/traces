{ mkGoProvider, ... }:

mkGoProvider {
  name = "claude";
  directory = ./.;
}
