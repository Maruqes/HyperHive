# Runtime Configuration & Autostart
Complete these steps after any installation guide (Solo, Normal, or Extra). They cover `.env` configuration for master/slave nodes and setting up automatic startup with PM2.

## 1. Prepare `.env` on the Master
1. Copy the template if needed:
   ```bash
   cd /path/to/HyperHive
   cp master/.env.example master/.env
   ```
2. Edit `master/.env` and set:
   - `MODE`: `prod` for production clusters, otherwise leave `dev`.
   - `QEMU_UID` / `QEMU_GID`: match the system `qemu` user. Discover with:
     ```bash
     id -u qemu
     id -g qemu
     ```


## 2. Prepare `.env` on Each Slave
1. Copy the template if needed:
   ```bash
   cp slave/.env.example slave/.env
   ```
2. Edit `slave/.env` and set:
   - `MASTER_IP`: management IP or resolvable hostname of the master.
   - `SLAVE_IP`: this slave’s management IP or hostname.
   - `OTHER_SLAVE*_IP`: optional peers for live migration; comment out unused lines.
   - `MODE`: `prod` in production.
   - `MACHINE_NAME`: unique identifier (e.g., `slave1`).
   - `VNC_MIN_PORT` / `VNC_MAX_PORT`: per-slave port range, non-overlapping across nodes.
   - `QEMU_UID` / `QEMU_GID`: same method as the master—usually the `qemu` system user.


## 3. Build Binaries (Master & Slaves)
Run these on every node after each code update.
```bash
cd /path/to/HyperHive
mkdir -p bin

cd master
go build -o ../bin/hyperhive-master ./main.go

cd ../slave
go build -o ../bin/hyperhive-slave ./main.go
```
Ensure Go 1.25+ is installed. Adjust paths if you prefer a different binary location (e.g., `/usr/local/bin`).

## 4. Install PM2 and Configure Autostart
Perform as root (`sudo -i`) on each node.

1. Install PM2 (skip if already present):
   ```bash
   sudo npm install -g pm2
   ```
2. From the HyperHive root, register the master process (on the master node):
   ```bash
   sudo pm2 start /path/to/HyperHive/bin/hyperhive-master --name hyperhive-master
   ```
   On each slave node start the slave binary:
   ```bash
   sudo pm2 start /path/to/HyperHive/bin/hyperhive-slave --name hyperhive-slave
   ```
3. Persist the PM2 process list and enable system boot integration:
   ```bash
   sudo pm2 save
   sudo pm2 startup systemd -u root --hp /root
   ```
   Follow the command printed by PM2 (usually another `pm2 startup` line to copy/paste).
4. Confirm services:
   ```bash
   sudo pm2 status
   sudo pm2 logs hyperhive-master      # or hyperhive-slave
   ```

## 5. Maintenance Tips
- Re-run the build and PM2 restart after pulling new code:
  ```bash
  cd /path/to/HyperHive/master && go build -o ../bin/hyperhive-master ./main.go
  sudo pm2 restart hyperhive-master
  ```
- Use `sudo pm2 delete <name>` to remove old definitions before re-registering binaries.
***
