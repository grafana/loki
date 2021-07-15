----
title: IP matcher
----

Consider the following logs,

```
3.180.71.3 - - [17/May/2015:08:05:32 +0000] "GET /downloads/product_1 HTTP/1.1" 304 0 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
80.91.33.133 - - [17/May/2015:08:05:14 +0000] "GET /downloads/product_1 HTTP/1.1" 304 0 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.16)"
46.4.66.76 - - [17/May/2015:08:05:45 +0000] "GET /downloads/product_1 HTTP/1.1" 404 318 "-" "Debian APT-HTTP/1.3 (1.0.1ubuntu2)"
93.180.71.3 - - [17/May/2015:08:05:26 +0000] "GET /downloads/product_1 HTTP/1.1" 404 324 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
```

How would you use LogQL to search for log lines with IP addresses?. Say single IP to start with? That's easy, we can use LogQL line filter (kinda like distributed grep)

```logql
{foo="bar"} |= "3.180.71.3"
```

will output following log lines.

```
3.180.71.3 - - [17/May/2015:08:05:32 +0000] "GET /downloads/product_1 HTTP/1.1" 304 0 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
93.180.71.3 - - [17/May/2015:08:05:26 +0000] "GET /downloads/product_1 HTTP/1.1" 404 324 "-" "Debian APT-HTTP/1.3 (0.8.16~exp12ubuntu10.21)"
```

but wait! what `93.180.71.3` is doing here?. Oh yea. it actually matches with what we queried for `3.180.71.3`. It can also match with other log lines which we don't actually want. The right option would be to use regexp to match the IP address(something like `|~"^3.180.71.3"`).

Now forget about single IP, what about range of IP addresses? What about IP subnet? Or even more interesting IPv6 addresses? (not easy to come up with regexp for all the use cases).

Luckily, LogQL comes with built-in support for IP matcher.

It supports both IPv4 and IPv6 addresses.

It takes following syntax

```
ip("<pattern>")
```

Where pattern can be one of the following.

1. Single IP - e.g: `ip("192.168.0.1")`, `ip("::1")`
2. IP Range - e.g: `ip("192.168.0.1-192.189.10.12")`, `ip("2001:db8::1-2001:db8::8")`
3. CIDR pattern - e.g: `ip("192.168.4.5/16")`, `ip("2001:db8::/32")`

IP matcher can be used in both Line Filter and Label Filter expressions.

Examples:
- Line Filter

```logql
`{ foo = "bar" }|=ip("192.168.4.5/16")
```

Following query return log lines that *doesn't* match with given IPv4 range

```logql
`{ foo = "bar" }!=ip("192.168.4.5-192.168.4.20")
```

- Label Filter

```logql
{ foo = "bar" }|logfmt|remote_addr=ip("2001:db8::1-2001:db8::8")|level="error"
```

Can also be chained multiple times. For example, following query match log lines with all IPv4 subnet `192.168.4.5/16` except the IP `192.168.4.2`

```logql
{ foo = "bar" }|logfmt|addr=ip("192.168.4.5/16")| addr!=ip("192.168.4.2")
```

When using as Line Filter, only `|=` and `!=` operations are allowed. When using as Label Filter, operations `=` and `!=` are allowed.
