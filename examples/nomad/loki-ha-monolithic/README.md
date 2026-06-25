# HA Monolithic deployment mode

This Nomad job will deploy Loki in
[highly available monolithic deployment mode](https://grafana.com/docs/loki/latest/get-started/deployment-modes/#ha-monolithic-mode)
with minimum dependencies, using TSDB and S3 backend and with the ability to scale.

## Usage

Have a look at the job file and Loki configuration file and change it to suite your environment.

### Run job

Inside directory with job run:

```shell
nomad run job.nomad.hcl
```

To deploy a different version change `variable.version` default value or specify
from command line:

```shell
nomad job run -var="version=3.7.2" job.nomad.hcl
```

### Scale Loki

Change `count` in job file in `group "loki"` and run:

```shell
nomad run job.nomad.hcl
```

or use Nomad CLI

```shell
nomad job scale loki write <count>
```
