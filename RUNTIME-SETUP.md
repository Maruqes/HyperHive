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

Run these steps on each node (as `root` or with `sudo`).

### 5.1 Install PM2

```bash
npm install -g pm2
```

---

### 5.2 Master Node: Master + Local Slave via `ecosystem.config.js`

Create the config file on the master:

```bash
cd /path/to/HyperHive/master
nano ecosystem.config.js
```

Insert:

```js
module.exports = {
  apps: [
    {
      name: "hyperhive-master",
      script: "./512SvMan",
      cwd: "/path/to/HyperHive/master",
      autorestart: true,
      max_restarts: 0,
      min_uptime: "10s",
      restart_delay: 5000,
      env: {
        NODE_ENV: "production",
      },
    },
    {
      name: "hyperhive-slave",
      script: "./slave",
      cwd: "/path/to/HyperHive/slave",
      autorestart: true,
      max_restarts: 0,
      min_uptime: "10s",
      restart_delay: 5000,
      env: {
        NODE_ENV: "production",
      },
    },
  ],
};
```

Start both processes:

```bash
cd /path/to/HyperHive/master
sudo pm2 start ecosystem.config.js
```

---

### 5.3 Slave Nodes: Slave Only via `ecosystem.config.js`

On each slave:

```bash
cd /path/to/HyperHive/slave
nano ecosystem.config.js
```

Insert:

```js
module.exports = {
  apps: [
    {
      name: "hyperhive-slave",
      script: "./slave",
      cwd: "/path/to/HyperHive/slave",
      autorestart: true,
      max_restarts: 0,
      min_uptime: "10s",
      restart_delay: 5000,
      env: {
        NODE_ENV: "production",
      },
    },
  ],
};
```

Start:

```bash
cd /path/to/HyperHive/slave
sudo pm2 start ecosystem.config.js
```

---

### 5.4 Persist Processes and Enable Boot Autostart

```bash
sudo pm2 save
sudo pm2 startup systemd -u root --hp /root
```

Follow any extra command printed by PM2 to finalize integration.

---

### 5.5 Status & Logs

```bash
sudo pm2 status
sudo pm2 logs hyperhive-master
sudo pm2 logs hyperhive-slave-local
sudo pm2 logs hyperhive-slave      # On slave nodes
```

---

## 6. Maintenance Tips

### Rebuild & Restart After Updating Code

**Master:**

```bash
cd /path/to/HyperHive/master
go build
sudo pm2 restart hyperhive-master
```

**Local slave (on master):**

```bash
cd /path/to/HyperHive/slave
go build
sudo pm2 restart hyperhive-slave-local
```

**Remote slaves:**

```bash
cd /path/to/HyperHive/slave
go build
sudo pm2 restart hyperhive-slave
```

### Re-register if Needed

```bash
sudo pm2 delete hyperhive-master
sudo pm2 delete hyperhive-slave-local
sudo pm2 delete hyperhive-slave
```

---
