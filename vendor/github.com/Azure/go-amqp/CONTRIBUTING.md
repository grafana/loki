# Contributing

This repo is no longer under active development. See [issue #205](https://github.com/vcabbage/amqp/issues/205) for details.

~~Whether it's code, documentation, and/or example, all contributions are appreciated.~~

~~To ensure a smooth process, here are some guidelines and expectations:~~

* ~~An issue should be created discussing any non-trivial change. Small changes, such as a fixing a typo, don't need an issue.~~
* ~~Ideally, an issue should describe both the problem to be solved and a proposed solution.~~
* ~~Please indicate that you want to work on the change in the issue. If you change your mind about working on an issue you are always free to back out. There will be no hard feelings.~~
* ~~Depending on the scope, there may be some back and forth about the problem and solution. This is intended to be a collaborative discussion to ensure the problem is adequately solved in a manner that fits well in the library.~~

~~Once you're ready to open a PR:~~

* ~~Ensure code is formatted with `gofmt`.~~
* ~~You may also want to peruse https://github.com/golang/go/wiki/CodeReviewComments and check that code conforms to the recommendations.~~
* ~~Tests are appreciated, but not required. The integration tests are currently specific to Microsoft Azure and require a number of credentials provided via environment variables. This can be a high barrier if you don't already have setup that works with the tests.~~
* ~~When you open the PR CI will run unit tests. Integration tests will be run manually as part of the review.~~
* ~~All PRs will be merged as a single commit. If your PR includes multiple commits they will be squashed together before merging. This usually isn't a big deal, but if you have any questions feel free to ask.~~

~~I do my best to respond to issues and PRs in a timely fashion. If it's been a couple days without a response or if it seems like I've overlooked something, feel free to ping me.~~

## Debugging

### Logging

To enable debug logging, build with `-tags debug`. This enables debug level 1 by default. You can increase the level by setting the `DEBUG_LEVEL` environment variable to 2 or higher. (Debug logging is disabled entirely without `-tags debug`, regardless of `DEBUG_LEVEL` setting.)

To add additional logging, use the `debug(level int, format string, v ...interface{})` function, which is similar to `fmt.Printf` but takes a level as it's first argument.

### Packet Capture

Wireshark can be very helpful in diagnosing interactions between client and server. If the connection is not encrypted Wireshark can natively decode AMQP 1.0. If the connection is encrypted with TLS you'll need to log out the keys.

Example of logging the TLS keys:

```go
// Create the file
f, err := os.OpenFile("key.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)

// Configure TLS
tlsConfig := &tls.Config{
    KeyLogWriter: f,
}

// Dial the host
const host = "my.amqp.server"
conn, err := tls.Dial("tcp", host+":5671", tlsConfig)

// Create the connections
client, err := amqp.New(conn,
    amqp.ConnSASLPlain("username", "password"),
    amqp.ConnServerHostname(host),
)
```

You'll need to configure Wireshark to read the key.log file in Preferences > Protocols > SSL > (Pre)-Master-Secret log filename.
