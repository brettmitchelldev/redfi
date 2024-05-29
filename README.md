
# redfi

RedFI acts as a proxy between the client and Redis with the capability
of injecting faults on the fly, based on the rules given by you.

## Features
- Simple to use. It is just a binary that you execute.
- Transparent to the client.
- Apply failure injection on custom conditions
- Limit failure injection to a percentage of the commands.
- Limit failure injection to certain clients only.
- Failure types:
  - Latency
  - Dropped connections
  - Empty responses

## How it Works
RedFI is a proxy that sits between the client and the actual Redis server. On every incoming command from the client, it checks the list of failure rules provided by you, and then it applies the first rule that matches the request.

# This is a fork (of a fork)
## Differences with upstream version

- Support for Go modules
    - From previous fork
    - Required if you want to use a modern version of Go
- Removed support for configuration from redis-cli
- Added support for response stream fault injection (original only supported request stream)
- Added support for raw byte-sequence matching in rules with `rawMatchAll` and `rawMatchAny`
- Added RESP awareness; rules are applied to individual RESP requests/responses (original applies them to the raw TCP stream)
- Added a Containerfile for building a `redfi` image (no ci/cd or public image, for now, this is only to build from a local copy of the source)
- Added support for logging:
    - `log` directive on rules for debugging/designing fault plans
    - Application logs for identifying issues in `redfi`
- Removed support for pooled connections to the Redis server. This was causing proxy transparency issues in applications with a large number of connections to Redis.

## Usage
Make sure you have go installed. `mise` is a great tool for this: `mise use --global go@latest`

Build: `go build github.com/brettmitchelldev/redfi/cmd`

Run the resulting binary:
```bash
$ ./redfi -addr 127.0.0.1:6380 -redis 127.0.0.1:6379 -api 127.0.0.1:8081
redis   127.0.0.1:6379
proxy   127.0.0.1:6380
control 127.0.0.1:8081
```

- **addr**: The address on which the proxy listens on for new connections.
- **redis**: Address of the actual Redis server to proxy commands/connections to.
- **plan**: Path to the json file that contains the rules/scenarios for fault injection.
- **api**: The address used when binding the HTTP API
- **log**: Designates log level. Use 'v' to see matching command names, and 'vv' to see matched commands and match counts. Leave unset for no silent.

## Rules options

### "command"
Matches on the command name. The value is serialized as RESP before matching against incoming requests.

E.g. `"command": "set my-key foo"` is converted to `*3\n$3\nset\n$6\nmy-key\n$3\nfoo`

Note that this is NOT a prefix match, nor is it a match on the exact command name.

This is the matching mechanism implemented by the original, and it doesn't seem to be super useful. It may get removed in the future.

### "rawMatch"
More useful, `rawMatch` allows you to craft specific patterns to match against Redis requests.

Keep in mind that Redis communicates using [RESP](https://redis.io/docs/latest/develop/reference/protocol-spec/), so if you want to match an exact command, you'll need to format it as a RESP snippet.

For example, to match a `set` command, you could do something like: `*3\n$3\nset\n`, which will match any len-3 array whose first element is the exact command name `set`.

### "client_addr"
Limits the effect of a rule to a particular client. Applies as a prefix.

### "percentage"
Limits the effect of the rule to the approximate percentage of matched requests.

### "delay"
Waits to send the request to Redis for the given number of milliseconds.

### "return_empty"
Returns an empty response. In RESP, this is represented by a null bulk string: `$-1\r\n` (read, bulk string of length -1).

### "return_err"
Returns an error with the value of `return_err` as the message.

### "drop"
Closes the client connection.

## HTTP Controller API

The HTTP controller API only modifies the plan in memory. Even if you are using a plan file, changes made with the HTTP API will not be persisted to disk.

### GET /rules

Returns the list of all active rules:

```bash
$ curl localhost:8081/rules
{"ok":true,"rules":[{"name":"test"}]}
```

### POST /rules

Creates a new rule.

```bash
$ curl \
   -X POST \
   -H 'Content-Type: application/json' \
   -d '{"name": "test", "rawMatch": ["ping"]}' \
   localhost:8081/rules

{"ok":true,"rules":[{"name":"test","rawMatch":["ping"]}]}
```

### GET /rules/:ruleName

Gets a single rule. Returns a response with the same array format as the list API.

```bash
$ curl localhost:8081/rules/test
{"ok":true,"rules":[{"name":"test"}]}
```

### DELETE /rules/:ruleName

Removes a rule from memory.

```bash
$ curl \
    -X DELETE \
    localhost:8081/rules/test

{"ok":true}
```

