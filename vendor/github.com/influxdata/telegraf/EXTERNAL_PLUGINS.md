# External Plugins

This is a list of plugins that can be compiled outside of Telegraf and used via the `execd` [input](plugins/inputs/execd), [output](plugins/outputs/execd), or [processor](plugins/processors/execd). 
Check out the [external plugin documentation](/docs/EXTERNAL_PLUGINS.md) for more information on writing and contributing a plugin. 

Pull requests welcome.


## Inputs
- [rand](https://github.com/ssoroka/rand) - Generate random numbers
- [twitter](https://github.com/inabagumi/twitter-telegraf-plugin) - Gather account information from Twitter accounts
- [youtube](https://github.com/inabagumi/youtube-telegraf-plugin) - Gather account information from YouTube channels
- [awsalarms](https://github.com/vipinvkmenon/awsalarms) - Simple plugin to gather/monitor alarms generated  in AWS.
- [octoprint](https://github.com/BattleBas/octoprint-telegraf-plugin) - Gather 3d print information from the octoprint API.
- [systemd-timings](https://github.com/pdmorrow/telegraf-execd-systemd-timings) - Gather systemd boot and unit timestamp metrics.

## Outputs
- [kinesis](https://github.com/morfien101/telegraf-output-kinesis) - Aggregation and compression of metrics to send Amazon Kinesis.
