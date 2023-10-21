#!/bin/sh

# Based on https://nfpm.goreleaser.com/tips/

if ! command -V systemctl >/dev/null 2>&1; then
  echo "Could not find systemd. Skipping system installation." && exit 0
else
    systemd_version=$(systemctl --version | awk '/systemd /{print $2}')
fi

on_rpm=""
if command -V rpm >/dev/null 2>&1; then
  on_rpm="true"
fi

cleanInstall() {
    printf "\033[32m Post Install of a clean install\033[0m\n"

    # Create the user
    if ! id loki > /dev/null 2>&1 ; then
        adduser --system --shell /bin/false "loki"
    fi

    # rhel/centos7 cannot use ExecStartPre=+ to specify the pre start should be run as root
    # even if you want your service to run as non root.
    if [ "${systemd_version}" -lt 231 ]; then
        printf "\033[31m systemd version %s is less then 231, fixing the service file \033[0m\n" "${systemd_version}"
        sed -i "s/=+/=/g" /etc/systemd/system/loki.service
    fi
    printf "\033[32m Reload the service unit from disk\033[0m\n"
    systemctl daemon-reload ||:
    printf "\033[32m Unmask the service\033[0m\n"
    systemctl unmask loki ||:
    printf "\033[32m Set the preset flag for the service unit\033[0m\n"
    systemctl preset loki ||:

    # If current distro is RPM-based, don't enable systemd service
    if [ -z "${on_rpm}" ]; then
      printf "\033[32m Set the enabled flag for the service unit\033[0m\n"
      systemctl enable loki ||:
      systemctl restart loki ||:
    fi
}

upgrade() {
    :
    # printf "\033[32m Post Install of an upgrade\033[0m\n"
}

action="$1"
if  [ "$1" = "configure" ] && [ -z "$2" ]; then
  # Alpine linux does not pass args, and deb passes $1=configure
  action="install"
elif [ "$1" = "configure" ] && [ -n "$2" ]; then
    # deb passes $1=configure $2=<current version>
    action="upgrade"
fi

case "${action}" in
  "1" | "install")
    cleanInstall
    ;;
  "2" | "upgrade")
    upgrade
    ;;
  *)
    # $1 == version being installed
    printf "\033[32m Alpine\033[0m"
    cleanInstall
    ;;
esac
