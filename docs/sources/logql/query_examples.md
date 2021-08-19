---
title: Query examples
weight: 40
---

# Query examples

Some useful query examples here.

## Log Query examples

### Examples that filter on IP address 

- Return log lines that are not within a range of IPv4 addresses:

    ```logql
    {job_name="myapp"} != ip("192.168.4.5-192.168.4.20")
    ```


- This example matches log lines with all IPv4 subnet values `192.168.4.5/16` except IP address `192.168.4.2`:

    ```logql
    {job_name="myapp"}
		| logfmt
		| addr = ip("192.168.4.5/16")
		| addr != ip("192.168.4.2")
    ```

### Examples that aid in security evaluation

- Extract the user and IP address of failed logins from Linux `/var/log/secure`

    ```logql
    {job="security"} 
        |~ "Invalid user.*"
        | regexp "(^(?P<user>\\S+ {1,2}){8})"
        | regexp "(^(?P<ip>\\S+ {1,2}){10})"
        | line_format "IP = {{.ip}}\tUSER = {{.user}}"
    ```
   
- Get successful logins from Linux `/var/log/secure`

    ```logql
    {job="security"}
        != "grafana_com"
        |= "session opened"
        != "sudo: "
        |regexp "(^(?P<user>\\S+ {1,2}){11})"
        | line_format "USER = {{.user}}"
    ```

## Metrics Query examples

- Return the per-second rate of all non-timeout errors
within the last minutes per host for the MySQL job,
and only include errors whose duration is above ten seconds.

    ```
    sum by (host) (rate({job="mysql"}
        |= "error" != "timeout"
        | json
        | duration > 10s [1m]))
    ```
