# SubStore development guide

## Project purpose

SubStore is a self-hosted Clash/Mihomo subscription manager. It combines manual proxy servers and subscription sources into a generated Clash configuration and private subscription URLs.

The current stack is deliberately small:

- Go backend in `main.go`
- SQLite data in `data/substore.db`
- Vanilla HTML, CSS, and JavaScript in `web/index.html`, `web/styles.css`, and `web/app.js`

Do not introduce a frontend framework or environment-variable configuration for normal user settings without an explicit request.

## Product vocabulary

- **Subscription**: an imported provider source. It can have match and mismatch filters.
- **Manual proxy**: an individually configured server.
- **Proxy group**: a Clash selection/failover group containing subscriptions, manual proxies, groups, or `DIRECT`.
- **Rule provider**: a maintained external Clash rule list. Its path is generated automatically.
- **Routing rule**: an ordered rule that sends traffic to a group, `DIRECT`, or `REJECT`.
- **Access key**: a private URL token used to fetch the generated configuration.
- **Client configuration**: generated Clash client settings such as LAN access, mode, DNS, and Fake IP. Keep this separate from runtime application settings.

## Data and configuration rules

- Add a SQLite migration for every new persisted field. Existing installations must migrate safely.
- User-created records must always be editable and deletable with explicit controls.
- Use stable database IDs for stored relationships. Renaming a subscription or group must not break memberships or rules.
- Subscription proxy definitions must preserve all original Clash fields. Do not rebuild imported nodes from only a partial set of fields; Shadowsocks requires `cipher`.
- Generated subscription configuration must remain valid YAML and valid Clash structure.
- Keep the generated order for imported proxies: `name`, `type`, `server`, `port`, `cipher`, `password`, then remaining source fields.
- Rule-provider and subscription local paths are automatic. Do not expose manual path inputs unless explicitly requested.
- HTTP listener port and public base URL are environment-managed. Do not add them back into Web UI settings.

## Current routing intent

- `CHEAP` contains 光喵 and is for high-volume traffic.
- `HOMEIP` contains 冲浪猫-家宽 and is for higher-requirement traffic.
- The combined subscription usage display mirrors the Cheap/光喵 subscription’s usage.

## UI rules

- Do not ship demo data, fake traffic metrics, or buttons that do nothing.
- Use clear human-facing labels. Never rely on ambiguous `...` menus for destructive actions.
- Use explicit Edit and Delete icons, with accessible labels and confirmation before deletion.
- Keep matching controls visually consistent: action buttons use the same size; use the shared SVG icon style.
- A control must visibly reflect its state when clicked, especially toggles and selectors.
- Do not let overlays, invisible inputs, or absolute positioning block unrelated controls.
- Keep page selection and display preferences persistent across refreshes when appropriate.
- Use centered empty states for empty subscriptions and proxy groups.
- Proxy groups support both Cards and One line layouts; the preference belongs under Client configuration → Display.

## Required verification

After changes, run at least:

```sh
node --check web/app.js
gofmt -w main.go
GOCACHE=/private/tmp/substore-gocache GOMODCACHE=/private/tmp/substore-modcache go vet ./...
GOCACHE=/private/tmp/substore-gocache GOMODCACHE=/private/tmp/substore-modcache go test ./...
GOCACHE=/private/tmp/substore-gocache GOMODCACHE=/private/tmp/substore-modcache go build -o substore .
```

For generated-config changes, fetch a local private subscription URL and validate that it parses as YAML. Check required protocol fields, especially the `cipher` on every Shadowsocks proxy.

For UI changes, verify the affected interactions in the browser when an authenticated session is available. Check desktop and narrow layouts, action buttons, toggles, modals, and copy controls. Do not claim an interaction works merely because the HTML renders.
