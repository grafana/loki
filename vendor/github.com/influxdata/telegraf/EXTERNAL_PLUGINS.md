# External Plugins

This is a list of plugins that can be compiled outside of Telegraf and used via the `execd` [input](plugins/inputs/execd), [output](plugins/outputs/execd), or [processor](plugins/processors/execd).
Check out the [external plugin documentation](/docs/EXTERNAL_PLUGINS.md) for more information on writing and contributing a plugin.

Pull requests welcome.

## Inputs

- [awsalarms](https://github.com/vipinvkmenon/awsalarms) - Simple plugin to gather/monitor alarms generated  in AWS.
- [octoprint](https://github.com/BattleBas/octoprint-telegraf-plugin) - Gather 3d print information from the octoprint API.
- [opcda](https://github.com/lpc921/telegraf-execd-opcda) - Gather data from [OPC Foundation's Data Access (DA)](https://opcfoundation.org/about/opc-technologies/opc-classic/) protocol for industrial automation.
- [open-hardware-monitor](https://github.com/marianob85/open_hardware_monitor-telegraf-plugin) - Gather sensors data provided by [Open Hardware Monitor](http://openhardwaremonitor.org)
- [plex](https://github.com/russorat/telegraf-webhooks-plex) - Listens for events from Plex Media Server [Webhooks](https://support.plex.tv/articles/115002267687-webhooks/).
- [rand](https://github.com/ssoroka/rand) - Generate random numbers
- [SMCIPMITool](https://github.com/jhpope/smc_ipmi) - Python script to parse the output of [SMCIPMITool](https://www.supermicro.com/en/solutions/management-software/ipmi-utilities) into [InfluxDB line protocol](https://docs.influxdata.com/influxdb/latest/reference/syntax/line-protocol/).
- [systemd-timings](https://github.com/pdmorrow/telegraf-execd-systemd-timings) - Gather systemd boot and unit timestamp metrics.
- [twitter](https://github.com/inabagumi/twitter-telegraf-plugin) - Gather account information from Twitter accounts
- [youtube](https://github.com/inabagumi/youtube-telegraf-plugin) - Gather account information from YouTube channels
- [Big Blue Button](https://github.com/bigblueswarm/bigbluebutton-telegraf-plugin) - Gather meetings information from [Big Blue Button](https://bigbluebutton.org/) server
- [dnsmasq](https://github.com/machinly/dnsmasq-telegraf-plugin) - Gather dnsmasq statistics from dnsmasq
- [ldap_org and ds389](https://github.com/falon/CSI-telegraf-plugins) - Gather statistics from 389ds and from LDAP trees.
- [x509_crl](https://github.com/jcgonnard/telegraf-input-x590crl) - Gather information from your X509 CRL files
- [s7comm](https://github.com/nicolasme/s7comm) - Gather information from Siemens PLC
- [net_irtt](https://github.com/iAnatoly/telegraf-input-net_irtt) - Gather information from IRTT network test
- [dht_sensor](https://github.com/iAnatoly/telegraf-input-dht_sensor) - Gather temperature and humidity from DHTXX sensors
- [oracle](https://github.com/bonitoo-io/telegraf-input-oracle) - Gather the statistic data from Oracle RDBMS
- [db2](https://github.com/bonitoo-io/telegraf-input-db2) - Gather the statistic data from DB2 RDBMS
- [apt](https://github.com/x70b1/telegraf-apt) - Check Debian for package updates.
- [knot](https://github.com/x70b1/telegraf-knot) - Collect stats from Knot DNS.
- [fritzbox](https://github.com/hdecarne-github/fritzbox-telegraf-plugin) - Gather statistics from [FRITZ!Box](https://avm.de/produkte/fritzbox/) router and repeater
- [linux-psi-telegraf-plugin](https://github.com/gridscale/linux-psi-telegraf-plugin) - Gather pressure stall information ([PSI](https://facebookmicrosites.github.io/psi/)) from the Linux Kernel
- [hwinfo](https://github.com/zachstence/hwinfo-telegraf-plugin) - Gather Windows system hardware information from [HWiNFO](https://www.hwinfo.com/)
- [libvirt](https://gitlab.com/warrenio/tools/telegraf-input-libvirt) - Gather libvirt domain stats, based on a historical Telegraf implementation [libvirt](https://libvirt.org/)
- [bacnet](https://github.com/JurajMarcin/telegraf-bacnet) - Gather statistics from BACnet devices, with support for device discovery and Change of Value subscriptions
- [tado](https://github.com/zoeimogen/tado-telegraf-plugin) - Gather zone temperature settings and current temperature/humidity readings from Tado
- [homekit](https://github.com/hdecarne-github/homekit-telegraf-plugin) - Gather smart home statistics from [HomeKit](https://en.wikipedia.org/wiki/HomeKit) devices via Home Hub automation

## Outputs

- [kinesis](https://github.com/morfien101/telegraf-output-kinesis) - Aggregation and compression of metrics to send Amazon Kinesis.
- [firehose](https://github.com/muhlba91/telegraf-output-kinesis-data-firehose) - Sends metrics in batches to Amazon Kinesis Data Firehose.
- [playfab](https://github.com/dgkanatsios/telegraftoplayfab) - Sends metrics to [Azure PlayFab](https://learn.microsoft.com/en-us/gaming/playfab/).

## Processors

- [geoip](https://github.com/a-bali/telegraf-geoip) - Add GeoIP information to IP addresses.
- [metadata](https://github.com/lawdt/metadata) - Appends metadata gathered from Openstack to metrics.
