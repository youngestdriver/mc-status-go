# mcutil
![](https://img.shields.io/github/languages/code-size/mcstatus-io/mcutil)
![](https://img.shields.io/github/issues/mcstatus-io/mcutil)
![](https://img.shields.io/github/license/mcstatus-io/mcutil)

A zero-dependency library for interacting with the Minecraft protocol in Go. Supports retrieving the status of any Minecraft server (Java or Bedrock Edition), querying a server for information, sending remote commands with RCON, and sending Votifier votes. Look at the examples in this readme or search through the documentation instead.

## Installation

```bash
go get github.com/mcstatus-io/mcutil/v4
```

## Documentation

https://pkg.go.dev/github.com/mcstatus-io/mcutil/v4

## Usage

### HTTP API

You can run an HTTP API server that wraps `status.Modern` and `status.Bedrock`.

```bash
go run ./cmd/api -listen :8080 -timeout 5s
```

Run with Docker:

```bash
docker build -t mc-status-go:latest .
docker run --rm -p 8080:8080 mc-status-go:latest
```

Run with Docker Compose:

```bash
docker compose up -d --build
```

`icon` is disabled by default. Set `icon=true` to include the `icon` field in the JSON response.
The provided `docker-compose.yml` enables this by setting `icon=true`.

Request format:

```text
GET /{server_type}/{server_ip}:{server_port}
```

Supported `server_type` values:
- `java`
- `bedrock`

Examples:

```bash
curl http://127.0.0.1:8080/java/play.example.com:25565
curl http://127.0.0.1:8080/bedrock/bedrock.example.com:19132
```

Response JSON shape:

```json
{
  "online": true,
  "host": "play.example.com",
  "port": 25565,
  "ip_address": "play.example.com",
  "eula_blocked": false,
  "retrieved_at": 1772785808566,
  "expires_at": 1772785868566,
  "srv_record": null,
  "version": {
    "name_raw": "1.20.1",
    "name_clean": "1.20.1",
    "name_html": "<span><span>1.20.1</span></span>",
    "protocol": 763
  },
  "players": {
    "online": 2,
    "max": 20,
    "list": [
      {
        "uuid": "00000000-0000-0000-0000-000000000000",
        "name_raw": "Player1",
        "name_clean": "Player1",
        "name_html": "<span><span>Player1</span></span>"
      }
    ]
  },
  "motd": {
    "raw": "Welcome to the server",
    "clean": "Welcome to the server",
    "html": "<span><span>Welcome to the server</span></span>"
  },
  "mods": [],
  "software": null,
  "plugins": []
}
```

### Status (1.7+)

Retrieves the status of the Java Edition Minecraft server. This method only works on netty servers, which is version 1.7 and above. An attempt to use on pre-netty servers will result in an error.

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/mcstatus-io/mcutil/v4/status"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    response, err := status.Modern(ctx, "demo.mcstatus.io")

    if err != nil {
        panic(err)
    }

    fmt.Println(response)
}
```

### Legacy Status (‹ 1.7)

Retrieves the status of the Java Edition Minecraft server. This is a legacy method that is supported by all servers, but only retrieves basic information. If you know the server is running version 1.7 or above, please use `Status()` instead.

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/mcstatus-io/mcutil/v4/status"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    response, err := status.Legacy(ctx, "demo.mcstatus.io")

    if err != nil {
        panic(err)
    }

    fmt.Println(response)
}
```

### Bedrock Status

Retrieves the status of the Bedrock Edition Minecraft server.

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/mcstatus-io/mcutil/v4/status"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    response, err := status.Bedrock(ctx, "demo.mcstatus.io")

    if err != nil {
        panic(err)
    }

    fmt.Println(response)
}
```

### Basic Query

Performs a basic query lookup on the server, retrieving most information about the server. Note that the server must explicitly enable query for this functionality to work.

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/mcstatus-io/mcutil/v4/query"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    response, err := query.Basic(ctx, "play.hypixel.net")

    if err != nil {
        panic(err)
    }

    fmt.Println(response)
}

```

### Full Query

Performs a full query lookup on the server, retrieving all available information. Note that the server must explicitly enable query for this functionality to work.

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/mcstatus-io/mcutil/v4/query"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    response, err := query.Full(ctx, "play.hypixel.net")

    if err != nil {
        panic(err)
    }

    fmt.Println(response)
}
```

### RCON

Executes remote console commands on the server. You must know the connection details of the RCON server, as well as the password.

```go
import "github.com/mcstatus-io/mcutil/v4/rcon"

func main() {
    client, err := rcon.Dial("127.0.0.1", 25575)

    if err != nil {
        panic(err)
    }

    if err := client.Login("mypassword"); err != nil {
        panic(err)
    }

    if err := client.Run("say Hello, world!"); err != nil {
        panic(err)
    }

    fmt.Println(<- client.Messages)

    if err := client.Close(); err != nil {
        panic(err)
    }
}
```

## Send Vote

Sends a Votifier vote to the specified server, typically used by server listing websites. The host and port must be known of the Votifier server, as well as the token or RSA public key generated by the server. This is for use on servers running Votifier 1 or Votifier 2, such as [NuVotifier](https://www.spigotmc.org/resources/nuvotifier.13449/).

```go
import (
    "context"
    "time"

    "github.com/mcstatus-io/mcutil/v4/vote"
    "github.com/mcstatus-io/mcutil/v4/options"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)

    defer cancel()

    err := vote.SendVote(ctx, "127.0.0.1", 8192, options.Vote{
        // General
        ServiceName: "my-service",    // Required
        Username:    "PassTheMayo",   // Required
        Timestamp:   time.Now(),      // Required
        Timeout:     time.Second * 5, // Required

        // Votifier 1
        PublicKey: "...", // Required

        // Votifier 2
        Token: "abc123", // Required
        UUID:  "",       // Optional
    })

    if err != nil {
        panic(err)
    }
}
```

## License

[MIT License](https://github.com/mcstatus-io/mcutil/blob/main/LICENSE)
