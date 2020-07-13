# Puma as a service

## Upstart

See `/tools/jungle/upstart` for Ubuntu's upstart scripts.

## Systemd

See [/docs/systemd](https://github.com/puma/puma/blob/master/docs/systemd.md).

## Init.d

Deprecatation Warning : `init.d` was replaced by `systemd` since Debian 8 and Ubuntu 16.04, you should look into [/docs/systemd](https://github.com/puma/puma/blob/master/docs/systemd.md) unless you are on an older OS.

See `/tools/jungle/init.d` for tools to use with init.d and start-stop-daemon.

## rc.d

See `/tools/jungle/rc.d` for FreeBSD's rc.d scripts
