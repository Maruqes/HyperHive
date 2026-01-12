# CrowdSec + Nginx Proxy Manager (iptables) on HyperHive

This guide installs CrowdSec on Fedora and protects Nginx Proxy Manager (running in Docker with `network_mode: host`) using the iptables firewall bouncer.

## Architecture

```
Internet
  ↓
Nginx Proxy Manager (Docker, host network)
  ↓ access logs
CrowdSec (host)
  ↓ decisions
iptables (kernel firewall)
  ↓
Blocked before reaching containers
```

## 1) Install CrowdSec on the host

```bash
curl -s https://install.crowdsec.net | sudo sh
sudo dnf install -y crowdsec
```

Enable and start:

```bash
sudo systemctl enable --now crowdsec
sudo systemctl status crowdsec
```

Check version:

```bash
crowdsec -version
```

## 2) Install the iptables firewall bouncer

```bash
sudo dnf install -y crowdsec-firewall-bouncer-iptables
```

Verify config files:

```bash
ls /etc/crowdsec/bouncers/
```

Expected file:

```
crowdsec-firewall-bouncer.yaml
```

## 3) Register the firewall bouncer

Generate the API key:

```bash
sudo cscli bouncers add hyperhive-fw
```

Edit the bouncer config:

```bash
sudo nano /etc/crowdsec/bouncers/crowdsec-firewall-bouncer.yaml
```

Set:

```yaml
api_url: http://127.0.0.1:8080/
api_key: YOUR_API_KEY_HERE
mode: iptables
```

Enable the service:

```bash
sudo systemctl enable --now crowdsec-firewall-bouncer
sudo systemctl status crowdsec-firewall-bouncer
```

## 4) Connect Nginx Proxy Manager logs

Logs path:

```
/your/path/HyperHive/master/npm-data/logs
```

Create an acquisition file:

```bash
sudo nano /etc/crowdsec/acquis.d/npm.yaml
```

Content:

```yaml
filenames:
  - /your/path/HyperHive/master/npm-data/logs/*_access.log
  - /your/path/HyperHive/master/npm-data/logs/fallback_access.log
labels:
  type: nginx
```

## 5) Install NPM parsers and scenarios

```bash
sudo cscli collections install crowdsecurity/nginx
sudo cscli collections install crowdsecurity/nginx-proxy-manager
sudo systemctl restart crowdsec
```

Verify ingestion:

```bash
sudo cscli metrics
```

## 6) Allow Wireguard VPN traffic

Allow UDP 51512 at the firewall:

```bash
sudo iptables -I INPUT 1 -p udp --dport 51512 -j ACCEPT
```

## 7) Test detection

```bash
for i in $(seq 1 200); do
  printf "request %s\n" "$i"
  curl -A "masscan" http://your-domain/
  printf "\n"
done
```

Check alerts and bans:

```bash
sudo cscli alerts list
sudo cscli decisions list
```

## 8) Test firewall blocking

```bash
sudo cscli decisions add -i 1.2.3.4 -t ban -d 2m
```

Verify iptables rules:

```bash
sudo iptables -L -n | grep CROWDSEC
```

## 9) Result

CrowdSec now parses NPM logs, detects scans, CVEs, brute-force attempts, syncs with the community blocklist, and blocks IPs in iptables before traffic reaches Docker or Nginx.
