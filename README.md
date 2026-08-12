# Fancontrol GUI

Small Go server for the fancontrol web UI. It uses the shared MikroTik UI bundle for the header, theme, icons, and translations; shared assets are never copied into this project.

The shared CSS is loaded from `mikrotik-ui-shared` (`/styles-modern.css`). The Docker build resolves the latest `main` revision automatically through `UI_SHARED_REV`; rebuild the image after shared UI changes. For local development, set `UI_SHARED_DIR` to the shared repository's `ui` directory.

## Modern preview

![Fancontrol GUI modern theme](./screenshots/fancontrol-modern.png)

The UI connects to the Proxmox host over SSH and installs a small standalone host controller. It writes `/etc/fancontrol-gui.conf`, `/usr/local/sbin/fancontrol-gui` and `fancontrol-gui.service`, then reloads the service. The controller reads hwmon temperature/PWM files directly, applies the selected three-point curve, and restores PWM ownership to BIOS/hardware when disabled. It does not require the Arc addon or any container-side hardware access.

## Run locally

```sh
UI_SHARED_DIR=/path/to/mikrotik-ui-shared/ui go run .
```

Then open <http://127.0.0.1:4173>.

The server also exposes `GET /healthz`. **Save & Apply**, **Fancontrol Off** and **Restart** execute the corresponding SSH actions on the Proxmox host. Before applying, the previous host files are backed up under `/root/fancontrol-gui-backup-YYYYMMDD-HHMMSS`.

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
