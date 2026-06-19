# Kiro-Go Plus

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker)](https://www.docker.com/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Convert Kiro accounts to an OpenAI / Anthropic compatible API service, with
improved regional account support and account JSON importing.

[English](README.md) | [中文](README_CN.md)

Kiro-Go Plus is maintained by
[lizethhilkeor5-art](https://github.com/lizethhilkeor5-art) and derived from
[Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go). The original
copyright notice is retained, while Kiro-Go Plus modifications are also
copyrighted by the current maintainer under the same MIT license.

## Features

- Anthropic `/v1/messages` & OpenAI `/v1/chat/completions`
- Multi-account pool with round-robin load balancing
- Auto token refresh, SSE streaming, Web admin panel
- Multiple auth: AWS Builder ID, IAM Identity Center (Enterprise SSO), SSO Token, local cache, credentials JSON
- Usage tracking, account import/export, i18n (CN / EN)
- Support configuring outbound proxy (SOCKS5 / HTTP)
- Region-aware Kiro management/runtime API endpoints
- Direct import of original snake_case account JSON arrays
- Direct import of Kiro Account Manager envelopes and enterprise `external_idp` full JSON
- Automatic CodeWhisperer profile resolution when `profileArn` is absent

## Quick Start

### Docker Compose (Recommended)

```bash
git clone https://github.com/lizethhilkeor5-art/kiro-go-plus.git
cd kiro-go-plus
mkdir -p data
docker-compose up -d
```

### Docker Run

```bash
docker run -d \
  --name kiro-go-plus \
  -p 8080:8080 \
  -e ADMIN_PASSWORD=your_secure_password \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/lizethhilkeor5-art/kiro-go-plus:latest
```

### Build from Source

```bash
git clone https://github.com/lizethhilkeor5-art/kiro-go-plus.git
cd kiro-go-plus
go build -o kiro-go-plus .
./kiro-go-plus
```

### Run on a Linux Server

Linux servers can run Kiro-Go Plus from source or Docker. Set
`ADMIN_PASSWORD` and keep the `data` directory persistent so accounts and
settings survive restarts.

```bash
git clone https://github.com/lizethhilkeor5-art/kiro-go-plus.git
cd kiro-go-plus
mkdir -p data
go build -o kiro-go-plus .
ADMIN_PASSWORD='your_secure_password' ./kiro-go-plus
```

Admin panel:

```text
http://<server-ip>:8080/admin
```

For long-running deployments, use `systemd`, `screen`, `tmux`, or Docker.

### Deploy on Zeabur

The repo already includes a `Dockerfile`, so it builds and runs on Zeabur out of the box.

**Option 1: Dashboard (one-click)**

1. Fork this repo to your GitHub account.
2. In Zeabur, create a new service and choose **Deploy from GitHub**, then select your fork.
3. Zeabur auto-detects the `Dockerfile` and builds the image.
4. In the **Networking** tab, expose port `8080` and bind a domain.
5. In the **Variables** tab, set at least `ADMIN_PASSWORD` (admin panel password).
6. Mount a Volume at `/app/data` if you want accounts / config to survive redeploys.

**Option 2: CLI**

```bash
npm i -g zeabur
zeabur auth login
zeabur deploy
```

> Run the commands from the project root. The CLI writes `.zeabur/context.json` to remember the target project / service — it contains personal IDs, so don't commit it.

Once the service is up, open `https://<your-domain>/admin` to log in.

Config is auto-created at `data/config.json`. Mount `/app/data` for persistence. The default admin password is `changeme` — override it via the `ADMIN_PASSWORD` env var or change it in the admin panel before going to production.

## Usage

Open `http://localhost:8080/admin`, log in, add accounts, then call the API:

```bash
# Claude
curl http://localhost:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "anthropic-version: 2023-06-01" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"Hello!"}]}'

# OpenAI
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer any" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello!"}]}'
```

### Account JSON Import Notes

Builder ID / IdC accounts can be imported from JSON containing `refreshToken`,
`clientId`, `clientSecret`, `region`, and `profileArn`.

Enterprise organization accounts are usually `external_idp` accounts and must
keep the full JSON metadata, especially:

- `refreshToken`
- `clientId`
- `tokenEndpoint`
- `issuerUrl`
- `scopes`
- `profileArn` when available

Do not convert enterprise accounts into the simplified seven-field format. That
can drop `tokenEndpoint` and cause refresh HTTP 400 errors or missing
`clientSecret` validation failures.

For the complete Kiro IDE export, Kiro-Go Plus import, and validation workflow,
see the Chinese guide: [Kiro JSON Export and Import Guide](docs/kiro-json-export-import-guide.md).

## Thinking Mode

Append a suffix (default `-thinking`) to the model name, e.g. `claude-sonnet-4.5-thinking`. Claude-compatible requests that include a top-level `thinking` config such as `{"type":"enabled","budget_tokens":2048}` or `{"type":"adaptive"}` also enable thinking mode automatically. Configure output format in the admin panel under Settings - Thinking Mode.

## Outbound Proxy

For users in restricted network regions, configure an outbound proxy in the admin panel under **Settings - Outbound Proxy Settings**. Supports SOCKS5 and HTTP proxies.

The setting takes effect immediately without restarting.

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONFIG_PATH` | Config file path | `data/config.json` |
| `ADMIN_PASSWORD` | Admin panel password (overrides config) | - |

## Contributing

Friendly discussion and pull requests are welcome. All changes to `main`
require a pull request and approval from the repository code owner. See
[CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Friend Links

- [LINUX DO](https://linux.do)

## Disclaimer

For educational and research purposes only. Not affiliated with Amazon, AWS, or Kiro. Users are responsible for complying with applicable terms of service and laws. Use at your own risk.

## License

[MIT](LICENSE). This derivative preserves the original Quorinex copyright
notice and adds the maintainer's copyright for Kiro-Go Plus modifications.
