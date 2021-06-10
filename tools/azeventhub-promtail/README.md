# azeventhub-promtail

This is a sample template for azeventhub-promtail - Below is a brief explanation of what we have generated for you:

```bash
.
├── Makefile                    <-- Make to automate build
├── README.md                   <-- This instructions file
├── hello-world                 <-- Source code for a lambda function
│   └── main.go                 <-- Lambda function code
└── template.yaml
```

## Requirements

* Azure Event Hub already configured with Administrator permission
* [Docker installed](https://www.docker.com/community-edition)
* [Golang](https://golang.org)

## Setup process

Provision an Azure Event Hub and start feeding data into it
### Installing dependencies & building the target 

The `go build` command is wrapped inside of the `Makefile`. To execute this simply run
 
```shell
make
```

### Usage

Currently this binary is designed to export Azure WAF access logs and firewall logs to a loki-push-api for loki ingestion and searching features

This repo will unwrap the JSON response from an Azure Event Hub in the format {"records":[{"data":"data"}]}, it removes the top layer "records" and shows the JSON logs

Currently in my testing Azure Event Hub will release batches of logs in minutely intervals all of which are captured in a single "records" Array as mentioned above

Currently unknown how other formats of data Azure collects, may need modifications