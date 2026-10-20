// Disposable dual-stack HTTP/TLS, WebSocket, TCP and UDP backends.
const http = require('node:http');
const https = require('node:https');
const tls = require('node:tls');
const net = require('node:net');
const dgram = require('node:dgram');
const crypto = require('node:crypto');
const fs = require('node:fs');
const members = new Map();
const credentials = fs.existsSync('/data/hh-fixture.key') ? {
  key: fs.readFileSync('/data/hh-fixture.key'), cert: fs.readFileSync('/data/hh-fixture.crt'),
} : null;
function startMember(index) {
  const address = index ? '::1' : '127.0.0.1';
  const label = `member-${index + 1}`;
  const handler = (req, res) => {
    res.setHeader('X-Member', label);
    res.setHeader('X-Request-Path', req.url);
    if (req.url.startsWith('/hold')) {
      res.setHeader('X-Accel-Buffering', 'no');
      res.flushHeaders();
      res.write('held:');
      setTimeout(() => res.end(label), 3000);
    } else res.end(label);
  };
  const web = http.createServer(handler);
  web.on('upgrade', (req, socket) => {
    const accept = crypto.createHash('sha1').update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
    socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ' + accept + '\r\n\r\n');
    socket.end(Buffer.concat([Buffer.from([0x81, Buffer.byteLength(label)]), Buffer.from(label)]));
  });
  web.listen(18080 + index, address);
  const servers = [web];
  if (credentials) {
    const secure = https.createServer({...credentials, SNICallback: (name, callback) => {
      if (name !== 'pool.test') return callback(new Error('unexpected backend SNI'));
      callback(null, tls.createSecureContext(credentials));
    }}, handler);
    secure.listen(18443 + index, address);
    servers.push(secure);
  }
  const tcp = net.createServer(socket => {
    let held = false;
    socket.once('data', data => {if (data.toString() === 'hold') {held = true; socket.write(label);}});
    setTimeout(() => {if (!held) socket.end(label);}, 20);
  });
  tcp.listen(19090 + index, address);
  const udp = dgram.createSocket(index ? 'udp6' : 'udp4');
  udp.on('message', (_, remote) => udp.send(label, remote.port, remote.address));
  udp.bind(19090 + index, address);
  members.set(index, [...servers, tcp, udp]);
}
startMember(0);
startMember(1);
http.createServer(async (req, res) => {
  const [, action, member] = req.url.split('/');
  const index = Number(member) - 1;
  if (action === 'stop' && members.has(index)) {
    await Promise.all(members.get(index).map(server => new Promise(resolve => server.close(resolve))));
    members.delete(index);
  } else if (action === 'start' && !members.has(index)) startMember(index);
  res.end('ok');
}).listen(19999, '127.0.0.1');
