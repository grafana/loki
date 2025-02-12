#!/usr/bin/env bash

# heading
# -----------------------------------
# Print standard heading
# -----------------------------------
heading() {
  local title="${1}"
  local message="${2}"
  local width="75"
  local orange="\\033[38;5;202m"
  local reset="\\033[0m"
  local bold="\\x1b[1m"
  echo ""
  echo -e "${orange}    ▒▒▓▓▒▒▒▓    ${reset}    ____                __                        _             _          "
  echo -e "${orange}  ▓▓▓        ▒  ${reset}   / ___| _ __   __ _  / _|  __ _  _ __    __ _  | |      __ _ | |__    ___ "
  echo -e "${orange}  ▒▓   ▒▒▒▒▓    ${reset}  | |  _ | '__| / _  || |_  / _  || '_ \\  / _  | | |     / _  || '_  \\ / __|"
  echo -e "${orange} ▒▓▓   ▒    ▒▒  ${reset}  | |_| || |   | (_| ||  _|| (_| || | | || (_| | | |___ | (_| || |_) | \\__\\"
  echo -e "${orange}  ▒▓▒       ▒▓  ${reset}   \\____||_|    \\__,_||_|   \\__,_||_| |_| \\__,_| |_____| \\__,_||_.__/ |___/"
  echo -e "${orange}    ▒▒▒   ▒▒▒   ${reset}  "
  echo -e "${orange}      ▒▒▒▒▒     ${reset}  $(repeat $(( ((width - ${#title}) - 2) / 2)) " ")${bold}$title${reset}"
  echo -e "${reset}                  $(repeat $(( ((width - ${#message}) - 2) / 2)) " ")$message${reset}"
  echo ""
}

# repeat
# -----------------------------------
# Repeat a Character N number of times
# -----------------------------------
repeat(){
  local times="${1:-80}"
  local character="${2:-=}"
  local start=1
  local range
  range=$(seq "$start" "$times")
  local str=""
  # shellcheck disable=SC2034
  for i in $range; do
    str="$str${character}"
  done
  echo "$str"
}

# lintWarning
# -----------------------------------
# Output a Lint Warning Message
# -----------------------------------
lintWarning() {
  local msg="${1}"
  local color_warning="\\x1b[33m"
  local color_reset="\\x1b[0m"
  local bold="\\x1b[1m"
  echo -e "    ‣ ${color_warning}${bold}[warn]${color_reset}  $msg"
}

# lintError
# -----------------------------------
# Output a Lint Error Message
# -----------------------------------
lintError() {
  local msg="${1}"
  local color_error="\\x1b[31m"
  local color_reset="\\x1b[0m"
  local bold="\\x1b[1m"
  echo -e "    ‣ ${color_error}${bold}[error]${color_reset} $msg"
}
