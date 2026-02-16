# godex Service + Tailscale Funnel Guide

This guide shows how to run `godex` as a persistent service and expose it publicly over HTTPS using Tailscale Funnel.

## Overview

Recommended pattern:
- `godex-proxy.service`: runs `godex` locally on loopback (`127.0.0.1`)
- `godex-funnel.service`: configures Tailscale Serve/Funnel to expose that local port on your `*.ts.net` domain via HTTPS

This keeps your app private locally while exposing only the HTTPS entrypoint.

## Port Strategy

Use a stable high-port range for internal listeners:
- Suggested range: `39000-39100`
- Example in this guide: `39001`

Why:
- Avoids privileged ports
- Avoids common app collisions
- Easy to reserve and document

## Prerequisites

- Tailscale installed and logged in on VPS
- `godex` binary installed (example path: `/usr/local/bin/godex`)
- Service account/user determined (examples below use user `cmd`)

## Template A: User-level systemd units (recommended first)

### `~/.config/systemd/user/godex-proxy.service`

```ini
[Unit]
Description=godex OpenAI-compatible proxy
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/godex proxy --log-requests --log-level info
Restart=on-failure
RestartSec=2
Environment=GODEX_PROXY_LISTEN=127.0.0.1:39001
# Optional API key file via systemd credential store
Environment=GODEX_PROXY_API_KEY=%d/godex_api_key
LoadCredential=godex_api_key:/home/cmd/.config/credstore/godex_api_key

[Install]
WantedBy=default.target
```

### `~/.config/systemd/user/godex-funnel.service`

```ini
[Unit]
Description=Tailscale Funnel for godex proxy
After=network-online.target godex-proxy.service
Wants=network-online.target
Requires=godex-proxy.service
PartOf=godex-proxy.service

[Service]
Type=oneshot
RemainAfterExit=yes
# Public HTTPS on ts.net -> local godex proxy
ExecStart=/usr/bin/tailscale serve --bg --https=443 http://127.0.0.1:39001
ExecStart=/usr/bin/tailscale funnel --bg --yes 443
ExecStop=/usr/bin/tailscale funnel 443 off
ExecStop=/usr/bin/tailscale serve reset

[Install]
WantedBy=default.target
```

### Enable and start

```bash
systemctl --user daemon-reload
systemctl --user enable --now godex-proxy.service godex-funnel.service
systemctl --user status godex-proxy.service godex-funnel.service --no-pager
```

### First-run Funnel auth (important)

On first use, Funnel may require interactive web auth/2FA approval. If this is pending, `systemctl --user enable --now ...` can appear to hang while `tailscale funnel ...` waits.

Bootstrap Funnel once manually:

```bash
tailscale serve --bg --https=443 http://127.0.0.1:39001
tailscale funnel --bg --yes 443
```

If prompted, open the URL, complete auth/2FA, then re-run:

```bash
systemctl --user restart godex-funnel.service
tailscale serve status
tailscale funnel status
```

Tip: use non-blocking enable/start during rollout:

```bash
systemctl --user enable --now --no-block godex-proxy.service godex-funnel.service
```

### Verify

```bash
tailscale serve status
tailscale funnel status
```

## Template B: Root-level Funnel unit (if user cannot access tailscaled)

Use this if user-level `tailscale ...` fails with permission errors on `tailscaled.sock`.

### `/etc/systemd/system/godex-funnel.service`

```ini
[Unit]
Description=Tailscale Funnel for godex proxy
After=network-online.target tailscaled.service
Wants=network-online.target
Requires=tailscaled.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/tailscale serve --bg --https=443 http://127.0.0.1:39001
ExecStart=/usr/bin/tailscale funnel --bg --yes 443
ExecStop=/usr/bin/tailscale funnel 443 off
ExecStop=/usr/bin/tailscale serve reset

[Install]
WantedBy=multi-user.target
```

### Enable and start (root)

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now godex-funnel.service
sudo systemctl status godex-funnel.service --no-pager
```

## Example: Current `godex` setup

If your `godex` unit uses:
- `GODEX_PROXY_LISTEN=127.0.0.1:39001`

Then your funnel mapping should be:
- `tailscale serve --https=443 http://127.0.0.1:39001`

## Operational Commands

### Restart stack

```bash
systemctl --user restart godex-proxy.service godex-funnel.service
```

### Stop public exposure only

```bash
systemctl --user stop godex-funnel.service
```

### Fully tear down serve/funnel config

```bash
tailscale funnel 443 off
tailscale serve reset
```

## Security Notes

- Keep `godex` bound to loopback (`127.0.0.1`) unless you explicitly need raw public ports.
- Funnel gives HTTPS termination; do not separately expose internal app port publicly unless required.
- Use API key auth in `godex` proxy when exposing publicly.
- Avoid committing secrets in unit files.

## Troubleshooting

### `Failed to connect to local Tailscale daemon ... operation not permitted`

Fix options:
1. Move funnel service to root-level systemd unit (`/etc/systemd/system`), or
2. Configure Tailscale operator permissions for your user.

### `godex-funnel` starts but URL not reachable

Check:
- `godex-proxy.service` active
- Local listener works: `curl -i http://127.0.0.1:39001`
- `tailscale funnel status` shows port 443 enabled
- VPS clock/time sane (TLS/cert edge cases)

### `systemctl --user enable --now ...` hangs during funnel startup

Likely cause:
- First-run Funnel auth/2FA is waiting for browser confirmation.

Fix:
1. Run `tailscale funnel --bg --yes 443` manually.
2. Complete auth in browser.
3. Restart unit: `systemctl --user restart godex-funnel.service`.

### Port conflicts

Pick another port in your reserved block (`39000-39100`) and update:
- `GODEX_PROXY_LISTEN`
- `tailscale serve ... http://127.0.0.1:<port>`
