# Runtime Configuration & Autostart
Complete these steps after finishing any install guide.

## 1. Prepare `.env` on the Master
1. Copy the template if needed:
   ```bash
   cd /path/to/HyperHive
   cp master/.env.example master/.env
   ```
2. Edit `master/.env` and set:
   - `MODE`: `prod` for production, otherwise leave `dev`.
   - `QEMU_UID` / `QEMU_GID`: match the system `qemu` user (check with `id -u qemu` / `id -g qemu`).
3. Lock down permissions:
   ```bash
   sudo chown root:root master/.env
   sudo chmod 600 master/.env
   ```

## 2. Prepare `.env` on Each Slave
1. Copy the template:
   ```bash
   cp slave/.env.example slave/.env
   ```
2. Edit `slave/.env` and set:
   - `MASTER_IP`: master management IP/hostname.
   - `SLAVE_IP`: this slave’s management IP/hostname.
   - `OTHER_SLAVE*_IP`: optional peers for migration (comment unused lines).
   - `MODE`, `MACHINE_NAME`, `VNC_MIN_PORT`, `VNC_MAX_PORT`, `QEMU_UID`, `QEMU_GID`.
3. Protect the file:
   ```bash
   sudo chown root:root slave/.env
   sudo chmod 600 slave/.env
   ```

## 3. Build Binaries (Manual)
Run on every node after code changes.
```bash
cd /path/to/HyperHive/master
go build

cd ../slave
go build
```

## 4. Manual Runtime (using Make)
Keep terminals open and run these to observe logs and ensure connectivity.
- On the master:
  ```bash
  cd /path/to/HyperHive/master
  sudo make run
  ```
- On each slave:
  ```bash
  cd /path/to/HyperHive/slave
  sudo make run
  ```
Leave them running until every slave prints a message similar to `Ok Master`, confirming the cluster is healthy.

## 5. Install PM2 and Configure Autostart
Perform on each node (as root or with `sudo`).
1. Install PM2 if needed:
   ```bash
   npm install -g pm2
   ```
2. Register processes **from inside each project folder** so `.env` files are available:
   - Master:
     ```bash
     cd /path/to/HyperHive/master
     pm2 start sudo --name hyperhive-master -- make run
     ```
   - Slaves:
     ```bash
     cd /path/to/HyperHive/slave
     pm2 start sudo --name hyperhive-slave -- make run
     ```
3. Persist and enable autostart:
   ```bash
   pm2 save
   pm2 startup systemd -u root --hp /root
   ```
   Follow any command PM2 prints to finalize boot integration.
4. Check status/logs:
   ```bash
   pm2 status
   pm2 logs hyperhive-master    # or hyperhive-slave
   ```

## 6. Maintenance Tips
- Rebuild/restart after pulling new code:
  ```bash
  cd /path/to/HyperHive/master && go build && pm2 restart hyperhive-master
  cd /path/to/HyperHive/slave && go build && pm2 restart hyperhive-slave
  ```
- Use `pm2 delete <name>` before re-registering if needed.
