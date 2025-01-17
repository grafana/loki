---
title: Matching IP addresses
menuTItle:  
description: Describes how LogQL supports matching IP addresses.
aliases: 
 - ../logql/ip/
weight: 600
---

# Matching IP addresses

LogQL supports matching IP addresses.

With logs such as

```
3.180.71.3 - - [17/May/2015:08:05:32 +0000] "GET /downloads/product_1 HTTP/1.1" 304 0 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
80.91.33.133 - - [17/May/2015:08:05:14 +0000] "GET /downloads/product_1 HTTP/1.1" 304 0 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.16)"
46.4.66.76 - - [17/May/2015:08:05:45 +0000] "GET /downloads/product_1 HTTP/1.1" 404 318 "-" "Debian APT-HTTP/1.3 (1.0.1ubuntu2)"
93.180.71.3 - - [17/May/2015:08:05:26 +0000] "GET /downloads/product_1 HTTP/1.1" 404 324 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
```

the LogQL line filter is not sufficient.
A line filter such as

```logql
{job_name="myapp"} |= "3.180.71.3"
```

also matches example IP addresses such as 93.180.71.3. A better choice uses a regexp: `|~"^3.180.71.3"`. This regexp does not handle IPv6 addresses, and it does not match a range of IP addresses.

The LogQL support for matching IP addresses handles both IPv4 and IPv6 single addresses, as well as ranges within IP addresses
and CIDR patterns.

Match IP addresses with the syntax: `ip("<pattern>")`.
The `<pattern>` can be:

-  A single IP address. Examples: `ip("192.0.2.0")`, `ip("::1")`
-  A range within the IP address. Examples: `ip("192.168.0.1-192.189.10.12")`, `ip("2001:db8::1-2001:db8::8")`
-  A CIDR specification. Examples: `ip("192.51.100.0/24")`, `ip("2001:db8::/32")`

The IP matching can be used in both line filter and label filter expressions.
When specifying line filter expressions, only the `|=` and `!=` operations are allowed.
When specifying label filter expressions, only the  `=` and `!=` operations are allowed.

- Line filter examples

    ```logql
    {job_name="myapp"} |= ip("192.168.4.5/16")
    ```

    Return log lines that do not match with an IPv4 range:

    ```logql
    {job_name="myapp"} != ip("192.168.4.5-192.168.4.20")
    ```

- Label filter examples

    ```logql
    {job_name="myapp"}
		| logfmt
		| remote_addr = ip("2001:db8::1-2001:db8::8")
		| level = "error"
    ```

    Filters can also be chained. This example matches log lines with all IPv4 subnet values `192.168.4.5/16` except IP address `192.168.4.2`:

    ```logql
    {job_name="myapp"}
		| logfmt
		| addr = ip("192.168.4.5/16")
		| addr != ip("192.168.4.2")
    ```

    This example use the conditional `or` and matches log lines with either, all IPv4 subnet values `192.168.4.0/24` OR all IPv4 subnet values `10.10.15.0/24`:

    ```logql
    {job_name="myapp"}
		| logfmt
		| addr = ip("192.168.4.0/24") or addr = ip("10.10.15.0/24")
    ```
