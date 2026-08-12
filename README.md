# Fancontrol GUI

Small Go server for the fancontrol web UI. It uses the shared MikroTik UI bundle for the header, theme, icons, and translations; shared assets are never copied into this project.

## Modern preview

![Fancontrol GUI modern theme](./screenshots/fancontrol-modern.png)

The UI connects to the Proxmox host over SSH and is designed to edit and apply the host-side fancontrol configuration without requiring console commands.

## Run locally

```sh
UI_SHARED_DIR=/path/to/mikrotik-ui-shared/ui go run .
```

Then open <http://127.0.0.1:4173>.

The server also exposes `GET /healthz`. The Proxmox/fancontrol command API can be added behind the existing UI actions without changing the shared UI contract.

## Docker

Build locally with the full Compose definition:

```sh
mkdir -p data
docker compose -f docker-container.yml up -d --build
```

Or use the published image:

```sh
mkdir -p data
docker compose -f docker-container.minimal.yml up -d
```

SSH values can be declared directly in Compose or an adjacent `.env` file. The application imports them into `/data/settings.json` at startup:

```env
PROXMOX_SSH_HOST=192.168.88.120
PROXMOX_SSH_PORT=22
PROXMOX_SSH_USER=root
PROXMOX_SSH_AUTH_METHOD=key
PROXMOX_SSH_KEY=-----BEGIN OPENSSH PRIVATE KEY-----
PROXMOX_SSH_PASSWORD=
FANCONTROL_MODE=Automatic (addon default)
FANCONTROL_ENABLED=true
```
