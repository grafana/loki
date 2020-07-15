# Sending Logs From AWS Elastic Container Service (ECS)

[ECS][ECS] is the fully managed container orchestration service by Amazon. Combined with [Fargate][Fargate] you can run your container workload without the need to provision your own compute resources. In this tutorial we will see how you can leverage [Firelens][Firelens] an AWS log router to forward all your logs and your workload metadata to a Loki instance.

After this tutorial you will able to query all your logs in one place using Grafana.

<!-- TOC -->

- [Sending Logs From AWS Elastic Container Service (ECS)](#sending-logs-from-aws-elastic-container-service-ecs)
    - [Requirements](#requirements)
    - [Setting up the ECS cluster](#setting-up-the-ecs-cluster)
    - [Creating your task definition](#creating-your-task-definition)
    - [Running your service](#running-your-service)

<!-- /TOC -->

## Requirements

Before we start you'll need:

- The [AWS CLI][aws cli] configured (run `aws configure`).
- A Grafana instance with a Loki data source already configured, you can use [GrafanaCloud][GrafanaCloud] free trial.
- A Subnet in VPC that is routable from the internet. (Follow those [instructions][create an vpc] if you need to create one).
- A [Security group][security group] of your choice for your containers. (Follow those [instructions][managing sg] if you need to create one).

For the sake of simplicity we'll use a GrafanaCloud Loki and Grafana instances, you can get an free account for this tutorial on our [website][GrafanaCloud], but all the steps are the same if you're running your own Open Source version of Loki and Grafana instances.

## Setting up the ECS cluster

To run containers with ECS you need an [ECS cluster][ecs cluster], we'll use a [Fargate][Fargate] cluster, but if you prefer to use an EC2 cluster all the given steps are still applicable.

Let's create the cluster with awscli:

```bash
aws ecs create-cluster --cluster-name ecs-firelens-cluster
```

We will also need an [IAM Role to run containers][ecs iam] with, let's create a new one and authorize [ECS][ECS] to endorse this role.

> You might already have this `ecsTaskExecutionRole` role in your AWS account if that's the case you can skip this step.

```bash
curl https://raw.githubusercontent.com/grafana/loki/master/docs/aws/ecs/ecs-role.json > ecs-role.json
aws iam create-role --role-name ecsTaskExecutionRole  --assume-role-policy-document file://ecs-role.json

{
    "Role": {
        "Path": "/",
        "RoleName": "ecsTaskExecutionRole",
        "RoleId": "AROA5FW5RZWLXFPU656SQ",
        "Arn": "arn:aws:iam::0000000000:role/ecsTaskExecutionRole",
        "CreateDate": "2020-07-09T14:51:49+00:00",
        "AssumeRolePolicyDocument": {
            "Version": "2012-10-17",
            "Statement": [
                {
                    "Effect": "Allow",
                    "Principal": {
                        "Service": [
                            "ecs-tasks.amazonaws.com"
                        ]
                    },
                    "Action": "sts:AssumeRole"
                }
            ]
        }
    }
}
```

Note down the [ARN][arn] of this new role, we'll use it later to create an ECS task.

Finally we'll give the [ECS task execution policy][ecs iam](`AmazonECSTaskExecutionRolePolicy`) to the created role, this will allows us to manage logs with [Firelens][Firelens]:

```bash
aws iam attach-role-policy --role-name ecsTaskExecutionRole --policy-arn "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
```

## Creating your task definition

Amazon [Firelens][Firelens] is a log router (usually `fluentd` or `fluentbit`) you run along the same task definition next to your application containers to route their logs to Loki.

In this example we will use [fluentbit][fluentbit] (with the [Loki plugin][fluentbit loki] installed) but if you prefer [fluentd][fluentd] make sure to check the [documentation][fluentd loki].

> We recommend you to use [fluentbit][fluentbit] as it's less resources consuming than [fluentd][fluentd].

Our [task definition][task] will be made of two containers, the [Firelens][Firelens] log router to send logs to Loki (`log_router`) and a sample application to generate log with (`sample-app`).

Let's download the task definition, we'll go through the most important parts.

```bash
curl https://raw.githubusercontent.com/grafana/loki/master/docs/aws/ecs/ecs-task.json > ecs-task.json
```

```json
 {
    "essential": true,
    "image": "grafana/fluent-bit-plugin-loki:1.5.0-amd64",
    "name": "log_router",
    "firelensConfiguration": {
        "type": "fluentbit",
        "options": {
            "enable-ecs-log-metadata": "true"
        }
    },
    "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
            "awslogs-group": "firelens-container",
            "awslogs-region": "us-east-2",
            "awslogs-create-group": "true",
            "awslogs-stream-prefix": "firelens"
        }
    },
    "memoryReservation": 50
},
```

The `log_router` container image is the [Fluent bit Loki docker image][fluentbit loki image] which contains the Loki plugin pre-installed. As you can see the `firelensConfiguration` type is set to `fluentbit` and we've also added `options` to enable ECS log metadata. This will be useful when querying your logs with Loki [LogQL][logql] label matchers.

> The `logConfiguration` is mostly there for debugging the fluent-bit container, but feel free to remove that part when you're done testing and configuring.

```json
 {
    "command": [
        "/bin/sh -c \"while true; do sleep 15 ;echo hello_world; done\""
    ],
    "entryPoint": ["sh","-c"],
    "essential": true,
    "image": "alpine:3.12",
    "logConfiguration": {
        "logDriver": "awsfirelens",
        "options": {
            "Name": "loki",
            "Url": "https://<userid>:<grafancloud apikey>@logs-prod-us-central1.grafana.net/loki/api/v1/push",
            "Labels": "{job=\"firelens\"}",
            "RemoveKeys": "container_id,ecs_task_arn",
            "LabelKeys": "container_name,ecs_task_definition,source,ecs_cluster",
            "LineFormat": "key_value"
        }
    },
    "name": "sample-app"
}
```

The second container is our `sample-app`, a simple [alpine][alpine] container that prints to stdout welcoming messages. To send those logs to Loki, we will configure this container to use the log driver `awsfirelens`.

Go ahead and replace the `Url` property with your [GrafanaCloud][GrafanaCloud] credentials, you can find them in your [account][grafanacloud account] in the Loki instance page. If you're running your own Loki instance replace completely the URL (e.g `http://my-loki.com:3100/loki/api/v1/push`).

All `options` of the `logConfiguration` will be automatically translated into [fluentbit ouput][fluentbit ouput]. For example, the above options will produce this fluent bit `OUTPUT` config section:

```conf
[OUTPUT]
    Name loki
    Match awsfirelens*
    Url https://<userid>:<grafancloud apikey>@logs-prod-us-central1.grafana.net/loki/api/v1/push
    Labels {job="firelens"}
    RemoveKeys container_id,ecs_task_arn
    LabelKeys container_name,ecs_task_definition,source,ecs_cluster
    LineFormat key_value
```

This `OUTPUT` config will forward logs to [GrafanaCloud][GrafanaCloud] Loki, to learn more about those options make sure to read the [documentation of the Loki output][fluentbit loki].
We've kept some interesting and useful labels such as `container_name`, `ecs_task_definition` , `source` and `ecs_cluster` but you can statically add more via the `Labels` option.

> If you want run multiple containers in your task, all of them needs a `logConfiguration` section, this give you the opportunity to add different labels depending on the container.

```json
{
    "containerDefinitions": [
     ...
    ],
    "cpu": "256",
    "executionRoleArn": "arn:aws:iam::00000000:role/ecsTaskExecutionRole",
    "family": "loki-fargate-task-definition",
    "memory": "512",
    "networkMode": "awsvpc",
    "requiresCompatibilities": [
        "FARGATE"
    ]
}
```

Finally, you need to replace the `executionRoleArn` with the [ARN][arn] of the role we created in the [first section](#Setting-up-the-ECS-cluster).

Once you've finished editing the task definition we can then run the command below to create the task:

```bash
aws ecs register-task-definition --region us-east-2 --cli-input-json  file://ecs-task.json
```

Now let's create and start a service.

## Running your service

To run the service you need to provide the task definition name `loki-fargate-task-definition:1` which is the combination of task family plus the task revision `:1`. You also need your own subnet and security group, you can replace respectively `subnet-306ca97d` and `sg-02c489bbdeffdca1d` in the command below and start the your service:

```bash
aws ecs create-service --cluster ecs-firelens-cluster \
--service-name firelens-loki-fargate \
--task-definition loki-fargate-task-definition:1 \
--desired-count 1 --region us-east-2 --launch-type "FARGATE" \
--network-configuration "awsvpcConfiguration={subnets=[subnet-306ca97d],securityGroups=[sg-02c489bbdeffdca1d],assignPublicIp=ENABLED}"
```

> Make sure public (`assignPublicIp`) is enabled otherwise ECS won't connect to the internet and you won't be able to pull external docker images.

You can now access the ECS console and you should see your task running. Now let's open Grafana and use explore with the Loki data source to explore our task logs. Enter the query `{job="firelens"}` and you should see our `sample-app` logs showing up as shown below:

![grafana logs firelens][grafana logs firelens]

Using the `Log Labels` dropdown you should be able to discover your workload via the ECS metadata, which is also visible if you expand a log line.

That's it ! Make sure to checkout the [LogQL][logql] to learn more about Loki powerful query language.

[create an vpc]: https://docs.aws.amazon.com/vpc/latest/userguide/vpc-subnets-commands-example.html
[ECS]: https://aws.amazon.com/ecs/
[Fargate]: https://aws.amazon.com/fargate/
[Firelens]: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/using_firelens.html
[GrafanaCloud]: https://grafana.com/signup/
[security group]: https://docs.aws.amazon.com/vpc/latest/userguide/VPC_SecurityGroups.html
[aws cli]: https://aws.amazon.com/cli/
[managing sg]: https://docs.aws.amazon.com/cli/latest/userguide/cli-services-ec2-sg.html
[ecs cluster]: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/clusters.html
[ecs iam]: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_execution_IAM_role.html
[arn]: https://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html
[task]: https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definitions.html
[fluentd loki]: https://github.com/grafana/loki/tree/master/cmd/fluentd
[fluentbit loki]: https://github.com/grafana/loki/tree/master/cmd/fluent-bit
[fluentbit]: https://fluentbit.io/
[fluentd]: https://www.fluentd.org/
[fluentbit loki image]: https://hub.docker.com/r/grafana/fluent-bit-plugin-loki
[logql]: https://github.com/grafana/loki/blob/master/docs/logql.md
[alpine]:https://hub.docker.com/_/alpine
[fluentbit ouput]: https://fluentbit.io/documentation/0.14/output/
[routing]: https://fluentbit.io/documentation/0.13/getting_started/routing.html
[grafanacloud account]: https://grafana.com/login
[grafana logs firelens]: ./ecs-grafana.png
[logql]: ../../../logql.md
