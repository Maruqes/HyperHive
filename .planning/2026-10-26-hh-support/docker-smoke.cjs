const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const { createRequire } = require("node:module");

const root = path.resolve(__dirname, "../..");
const WebSocket = createRequire(path.join(root, "../HyperHiveFront/HyperHive/package.json"))("ws");
const unique = `${Date.now()}-${process.pid}`;
const name = `hh-support-validation-${unique}`;
const volume = `${name}-data`;
const image = process.env.SUPPORT_SMOKE_IMAGE || `hyperhive-support:codex-validation-${unique}`;
const label = "io.hyperhive.validation";
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "hh-support-validation-"));
const secret = crypto.randomBytes(32).toString("base64url");
const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const docker = (...args) => execFileSync("docker", args, { encoding: "utf8", timeout: 300000 });
let containerCreated = false;
let volumeCreated = false;
let socket;

async function main() {
  const listener = net.createServer();
  await new Promise((resolve) => listener.listen(0, "127.0.0.1", resolve));
  const port = listener.address().port;
  await new Promise((resolve) => listener.close(resolve));
  const base = `http://127.0.0.1:${port}`;
  const envfile = path.join(temporary, "support.env");
  fs.writeFileSync(envfile, `SUPPORT_LISTEN=127.0.0.1:${port}\nSUPPORT_DB_PATH=/data/support.db\nSUPPORT_PROXY_SECRET=${secret}\n`, { mode: 0o600 });
  if (!process.env.SUPPORT_SMOKE_IMAGE) {
    console.log(`Building ${image}`);
    execFileSync("docker", ["build", "--network=host", "-t", image, path.join(root, "support")], { stdio: "inherit", timeout: 600000 });
  }
  docker("volume", "create", "--label", `${label}=${unique}`, volume);
  volumeCreated = true;
  const start = () => docker("run", "-d", "--name", name, "--label", `${label}=${unique}`, "--network", "host", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--restart", "unless-stopped", "--env-file", envfile, "--mount", `type=volume,source=${volume},target=/data`, image);
  start();
  containerCreated = true;
  const inspect = () => JSON.parse(docker("inspect", name))[0];
  const config = inspect();
  assert.equal(config.Config.User, "10001:10001");
  assert.equal(config.HostConfig.NetworkMode, "host");
  assert.equal(config.HostConfig.ReadonlyRootfs, true);
  assert.deepEqual(config.HostConfig.CapDrop, ["ALL"]);
  assert(config.HostConfig.SecurityOpt.includes("no-new-privileges:true"));
  assert.equal(config.HostConfig.RestartPolicy.Name, "unless-stopped");
  assert(config.Mounts.some((mount) => mount.Name === volume && mount.Destination === "/data" && mount.RW));
  async function request(method, route, body, status = 200, headers = { "X-HH-Support-Key": secret }) {
    const response = await fetch(base + route, { method, headers: { "Content-Type": "application/json", ...headers }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(5000) });
    assert.equal(response.status, status, `${method} ${route} status`);
    return status === 204 ? null : response.json();
  }
  async function healthy() {
    for (let attempt = 0; attempt < 100; attempt++) {
      try { assert.equal((await request("GET", "/healthz")).status, "ok"); return; } catch { await delay(100); }
    }
    throw new Error("Container did not become healthy within 10 seconds");
  }
  await healthy();
  docker("exec", name, "/usr/local/bin/hh-support", "--healthcheck");
  console.log("PASS runtime hardening, loopback HTTP and CLI healthcheck");
  await request("GET", "/apps", undefined, 401, {});
  await request("POST", "/ingest/", { heartbeat: true }, 401, {});
  const tabs = await request("GET", "/tabs");
  const created = await request("POST", "/apps", { name: "Docker validation", tab_id: tabs[0].id }, 201);
  assert(created.app.id && created.secret);
  const frames = [];
  socket = new WebSocket(base.replace("http:", "ws:") + "/ws", { headers: { "X-HH-Support-Key": secret } });
  socket.on("error", () => {});
  socket.on("message", (message) => frames.push(JSON.parse(message.toString())));
  async function waitFrame(type) {
    for (let attempt = 0; attempt < 100; attempt++) {
      const frame = frames.find((item) => item.type === type);
      if (frame) return frame;
      await delay(50);
    }
    throw new Error(`Missing ${type} WebSocket frame`);
  }
  await waitFrame("ready");
  const values = { number: 17.5, text: "ready", boolean: true, log: { message: "ready", severity: "info" }, event: { name: "startup", fields: { build: 1 } } };
  const credentials = { Authorization: `Bearer ${created.secret}`, "X-App-ID": created.app.id };
  const result = await request("POST", "/ingest", { heartbeat: true, streams: Object.keys(values).map((type) => ({ name: type, type })), samples: Object.entries(values).map(([stream, value]) => ({ stream, value })) }, 200, credentials);
  assert.equal(result.accepted, 5);
  for (const kind of ["changed", "samples", "state"]) await waitFrame(kind);
  assert.equal((await waitFrame("samples")).samples.length, 5);
  for (const [stream, value] of Object.entries(values)) {
    const history = await request("GET", `/apps/${created.app.id}/samples?stream=${stream}`);
    assert.equal(history.samples.length, 1);
    assert.deepEqual(history.samples[0].value, value);
  }
  assert(!(JSON.stringify(await request("GET", "/apps"))).includes(created.secret));
  console.log("PASS trust boundary, five typed streams, WebSocket ready/changed/samples/state and credential redaction");
  socket.close();
  docker("restart", name);
  await healthy();
  docker("rm", "-f", name);
  containerCreated = false;
  start();
  containerCreated = true;
  await healthy();
  docker("exec", name, "/usr/local/bin/hh-support", "--healthcheck");
  const restored = await request("GET", `/apps/${created.app.id}`);
  assert.equal(restored.app.id, created.app.id);
  assert.equal(restored.streams.length, 5);
  assert.equal((await request("GET", `/apps/${created.app.id}/samples?stream=number`)).samples[0].value, 17.5);
  await request("POST", "/ingest", { heartbeat: true }, 200, credentials);
  for (let attempt = 0; attempt < 100 && inspect().State.Health.Status !== "healthy"; attempt++) await delay(200);
  assert.equal(inspect().State.Health.Status, "healthy");
  console.log(`PASS restart/recreation persistence, sender credentials and Docker health status; image ${image}`);
}

main().catch((error) => { console.error(`FAIL ${error.message}`); process.exitCode = 1; }).finally(() => {
  socket?.terminate();
  try {
    if (containerCreated && JSON.parse(docker("inspect", name))[0].Config.Labels[label] === unique) docker("rm", "-f", name);
    if (volumeCreated && JSON.parse(docker("volume", "inspect", volume))[0].Labels[label] === unique) docker("volume", "rm", volume);
    console.log("Cleaned this run's disposable container and volume");
  } finally { fs.rmSync(temporary, { recursive: true, force: true }); }
});
