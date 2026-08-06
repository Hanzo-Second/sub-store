# SubStore

Self-hosted Clash/Mihomo subscription management with manual proxies, routing rules, generated Clash YAML, and private subscription URLs.

## Quick deployment with Docker and Nginx

The Compose service listens on `127.0.0.1:8080` by default, so it is reachable by Nginx but is not exposed directly to the internet.

1. Create the deployment configuration:

   ```sh
   cp .env.example .env
   ```

2. Set the exact public origin in `.env`:

   ```dotenv
   SUBSTORE_BIND_ADDRESS=127.0.0.1
   SUBSTORE_PORT=8080
   SUBSTORE_BASE_URL=https://sub.example.com
   ```

   `SUBSTORE_BASE_URL` must include `https://`, must match the domain clients use, and must not have a trailing slash.

3. Build and start SubStore:

   ```sh
   docker compose up --build -d
   docker compose logs --tail=100 substore
   ```

4. Copy [`deploy/nginx/substore.conf`](deploy/nginx/substore.conf) into the Nginx configuration, replace `sub.example.com`, enable HTTPS with your normal certificate tooling, and reload Nginx.

5. Open the public domain and create the administrator account.

The SQLite database is stored in the `substore-data` Docker volume. `docker compose down` preserves it; do not use `docker compose down -v` unless you intend to delete all application data.

## Updating

```sh
git pull --ff-only
docker compose up --build -d
```

## Local development

Requires Go 1.26 or newer:

```sh
go run .
```

The server creates `data/substore.db` and serves the embedded frontend at [http://localhost:8080](http://localhost:8080).

Runtime listener settings are environment-managed:

- `SUBSTORE_PORT` — HTTP port inside the process
- `SUBSTORE_BASE_URL` — exact externally visible origin

All other application and generated-client settings are managed in the web UI and persisted in SQLite.

## Repository layout

```text
.
├── main.go                 # HTTP server, persistence, and config generation
├── service_rules.go        # Curated built-in service routing rules
├── main_test.go            # Backend and generated-config tests
├── web/                    # Embedded vanilla HTML, CSS, and JavaScript
├── deploy/nginx/           # Reverse-proxy example
├── docs/                   # Architecture source material
├── Dockerfile
└── compose.yaml
```

## Features

- First-run administrator setup and cookie sessions
- Manual proxies for VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, SOCKS5, and HTTP
- Subscription import, filtering, scheduling, usage history, and updates
- Proxy groups, routing rules, service rules, and external rule providers
- Generated Clash-compatible YAML and private access-key URLs
- SQLite persistence and Docker Compose deployment
