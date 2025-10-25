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
THIS IS A MUST DO IT SETS PASSWORD BETWEEN SLAVES NEEDS TO BE RAN AT LEAST ONCE AND EVERY TIME A NEW SLAVE IS ADDED
- On the master:
  ```bash
  cd /path/to/HyperHive/master
  make
  ```
- On each slave:
  ```bash
  cd /path/to/HyperHive/slave
  make
  ```
Leave them running until every slave prints a message similar to `Ok Master`, confirming the cluster is healthy.

## 5. Install PM2 and Configure Autostart
Perform on each node (as root or with `sudo`).
1. Install PM2 if needed:
   ```bash
   npm install -g pm2
   ```
2. Register processes; PM2 will change directories and invoke `make` for you:
   - Master:
     ```bash
     sudo pm2 start bash --name hyperhive-master -- -lc "cd /path/to/HyperHive/master && ./512SvMan"
     ```
   - Slaves:
     ```bash
     sudo pm2 start bash --name hyperhive-slave -- -lc "cd /path/to/HyperHive/slave && ./slave"
     ```
3. Persist and enable autostart:
   ```bash
   sudo pm2 save
   sudo pm2 startup systemd -u root --hp /root
   ```
   Follow any command PM2 prints to finalize boot integration.
4. Check status/logs:
   ```bash
   sudo pm2 status
   sudo pm2 logs hyperhive-master    # or hyperhive-slave
   ```

## 6. Maintenance Tips
- Rebuild/restart after pulling new code:
  ```bash
  cd /path/to/HyperHive/master && go build && sudo pm2 restart hyperhive-master
  cd /path/to/HyperHive/slave && go build && sudo pm2 restart hyperhive-slave
  ```
- Use `sudo pm2 delete <name>` before re-registering if needed.
