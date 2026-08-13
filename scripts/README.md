# Host controller scripts

These files are installed on the Proxmox host by the Fancontrol GUI over SSH.

## Manual installation

Run as `root` on the target host:

```sh
./install.sh
```

The installer:

- installs `fancontrol-gui.sh` as `/usr/local/sbin/fancontrol-gui`;
- installs `fancontrol-gui.service` as `/etc/systemd/system/fancontrol-gui.service`;
- disables the legacy `fancontrol.service` to prevent competing PWM control;
- reloads systemd and starts the GUI controller only when `/etc/fancontrol-gui.conf` contains `ENABLED=1`.

The GUI normally installs these files itself over SSH. Manual installation is useful for recovery or offline setup.

When the controller stops, it returns supported `it87` PWM channels to automatic hardware PWM mode (`pwm_enable=2`).
