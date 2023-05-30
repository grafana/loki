# Monolithic mode

This Nomad job will deploy Loki in
[monolithic mode](https://grafana.com/docs/loki/latest/fundamentals/architecture/deployment-modes/#monolithic-mode)
with minimum dependencies, using boltdb-shipper and S3 backend and with the
ability to scale.

## Usage

Have a look at the job file and Loki configuration file and change to suite your
environment.

### Run job

Inside directory with job run:

```shell
nomad run job.nomad.hcl
```

To deploy a different version change `variable.version` default value or
specify from command line:

```shell
nomad job run -var="version=2.7.5" job.nomad.hcl
```

### Scale Loki

Change `count` in job file in `group "loki"` and run:

```shell
nomad run job.nomad.hcl
```

or use Nomad CLI

```shell
nomad job scale loki loki <count>
```
