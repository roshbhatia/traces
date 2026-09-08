{ mkGoProvider, ... }:

mkGoProvider {
  name = "gate";
  directory = ./.;
}
