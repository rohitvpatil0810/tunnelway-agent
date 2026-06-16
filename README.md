# tunnelway-agent

Tunnelway agent forwards public traffic to your local service.

## Setup

Run the one-time interactive setup to create config:

```bash
tunnelway setup
```

This writes config at:

```text
$HOME/.config/tunnelway-agent/config.yaml
```

Current config schema:

```yaml
server_url: wss://your-server.example.com
server_path: /_ws/agent
```

Note: setup and `--server-url` accept only scheme and host. `server_path` is written into config automatically and reused at runtime.

## Run

The local port is mandatory at runtime:

```bash
tunnelway --port 3000
```

Optional runtime override for server URL:

```bash
tunnelway --port 3000 --server-url wss://override.example.com
```

Resolution order for `server_url`:

1. `--server-url` flag (runtime only)
2. `server_url` from config file
3. error asking you to run `tunnelway setup`
