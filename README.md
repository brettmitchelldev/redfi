
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
- Apply faults to either the request on the way out to the server, or the response on the way back to the client.

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
Make sure you have go installed. [`mise`](https://github.com/jdx/mise) is a great tool for this: `mise use --global go@latest`

Build: `go build github.com/brettmitchelldev/redfi/cmd`

Run the resulting binary:
```bash
$ ./redfi -addr 127.0.0.1:6380 -redis 127.0.0.1:6379
redis   127.0.0.1:6379
proxy   127.0.0.1:6380
```

- **addr**: Proxy listen address. Real clients should connect to this address.
- **redis**: Address of the actual Redis server to proxy commands/connections to.
- **plan**: Path to the json file that contains the rules/scenarios for fault injection.
- **log**: Designates log level. Use 'v' to see matching command names, and 'vv' to see matched commands and match counts. Leave unset for silent.

## Plan configuration

A `redfi` fault plan is a JSON file with the following properties:
- `requestRules`: Rule definitions applied to the request stream going from the client to the server
- `responseRules`: Rule definitions applied to the response stream going from the server to the client

## Rule directives (request or reply)

### Match directives
Match directives apply using a logical "and"; all match directives on a rule must succeed for the rule to match the given message.

#### `command`
Matches on the command name. Note that `"command": "set"` is _not_ the same as `"rawMatchAll": ["set"]`.

The `command` example limits itself to matching exact command names only, whereas the `rawMatchAll` example will match even if `set` is found in the command arguments.

#### `rawMatchAny` / `rawMatchAll`
`rawMatchAny` and `rawMatchAll` allow you to craft exact substring patterns to match against Redis requests and responses.

As the names imply:
- `rawMatchAny` matches if at least one of its array members is found in a message
- `rawMatchAll` matches only if all of its array members is found in a message

Keep in mind that Redis communicates using [RESP](https://redis.io/docs/latest/develop/reference/protocol-spec/), so if you want to match an exact command, you'll need to format it as a RESP snippet.

For example, to match a `set` command, you could do something like: `*3\r\n$3\r\nset\r\n`, which will match any len-3 array whose first element is the exact command name `set`.

#### `clientAddr`
Limits the effect of a rule to a particular client. Matches against the client's address, operating as a prefix.

#### `clientName`
Limits the effect of a rule to a particular client by the value given to `CLIENT SETNAME`. Applies as an exact match. Rejects clients with no client name value.

#### `percentage`
Limits the effect of the rule to the approximate percentage of matched requests.

#### `alwaysMatch`
Forces the rule to always match, regardless of other match directives. Only evaluated once the `alwaysMatch` rule is reached in the prioritized list of rules. If you have another rule that matches first, `alwaysMatch` will not apply.

### Action directives

#### `log`
Logs the full rule as JSON, and logs the full matched message.

#### `delay`
Waits to proxy the message for the given number of milliseconds.

#### `returnEmpty`
Returns an empty response. In RESP, this is represented by a null bulk string: `$-1\r\n` (read, bulk string of length -1).

#### `returnErr`
Returns an error with the value of `returnErr` as the message.

#### `drop`
Closes the client connection.

