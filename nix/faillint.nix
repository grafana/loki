{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule rec {
  pname = "faillint";
  version = "1.11.0";

  src = fetchFromGitHub {
    owner = "fatih";
    repo = "faillint";
    rev = "v${version}";
    sha256 = "ZSTeNp8r+Ab315N1eVDbZmEkpQUxmmVovvtqBBskuI4=";
  };

  vendorSha256 = "5OR6Ylkx8AnDdtweY1B9OEcIIGWsY8IwTHbR/LGnqFI=";
  doCheck = false;
}
