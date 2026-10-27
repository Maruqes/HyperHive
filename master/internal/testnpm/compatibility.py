#!/usr/bin/env python3
"""Exact-image NPM compatibility fixture. No existing containers/data are modified."""
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import time
import uuid

import re
CONFIG = pathlib.Path(__file__).resolve().parents[2] / 'npm/upstream_config.go'
IMAGE = re.search(r'const UpstreamImageDigest = "([^"]+)"', CONFIG.read_text())[1]
TEMPLATES = ['/app/templates/proxy_host.conf', '/app/templates/_location.conf',
             '/app/templates/stream.conf', '/etc/nginx/conf.d/include/proxy.conf']


def command(*args, check=True):
    result = subprocess.run(args, text=True, capture_output=True, timeout=300)
    if check and result.returncode:
        raise RuntimeError(f'{args[:2]} failed: {result.stderr[-3000:]} {result.stdout[-3000:]}')
    return result


class Fixture:
    def __init__(self, root):
        self.root = root
        self.name = 'hh-upstream-proof-' + uuid.uuid4().hex[:12]

    def node(self, code):
        return command('docker', 'exec', self.name, 'node', '-e', code).stdout.strip()

    def api(self, method, path, body=None, token=''):
        # Transport credentials remain in memory and disposable container arguments.
        return json.loads(self.node(f'''(async()=>{{const r=await fetch('http://127.0.0.1:81/api{path}',
        {{method:{json.dumps(method)},headers:{{'Content-Type':'application/json',Authorization:'Bearer '+{json.dumps(token)}}},
        body:{json.dumps(json.dumps(body)) if body is not None else 'undefined'}}});
        const t=await r.text();if(!r.ok)throw Error(r.status+': '+t);console.log(t);}})().catch(e=>{{console.error(e.message);process.exit(1)}})'''))

    def write(self, path, content):
        self.node(f"require('node:fs').mkdirSync(require('node:path').dirname({json.dumps(path)}),{{recursive:true}});require('node:fs').writeFileSync({json.dumps(path)},{json.dumps(content)});")

    def probe(self, protocol, path='/'):
        if protocol == 'http':
            code = f"require('node:http').get({{host:'127.0.0.1',path:{json.dumps(path)},headers:{{Host:'pool.test'}}}},r=>{{let b='';r.on('data',v=>b+=v);r.on('end',()=>console.log(b))}}).setTimeout(2000,()=>process.exit(2));"
        elif protocol == 'tcp':
            code = "const s=require('node:net').connect(16060,'127.0.0.1');s.on('data',b=>console.log(b.toString()));s.setTimeout(2000,()=>process.exit(2));"
        else:
            code = "const s=require('node:dgram').createSocket('udp4');s.on('message',b=>{console.log(b.toString());s.close()});s.send('probe',16060,'127.0.0.1');setTimeout(()=>process.exit(2),2000).unref();"
        return self.node(code)

    def run(self):
        command('docker', 'run', '-d', '--name', self.name, '--network', 'none',
                '-e', 'INITIAL_ADMIN_EMAIL=fixture@example.test', '-e', 'INITIAL_ADMIN_PASSWORD=Disposable-fixture-Only123!',
                '-e', 'DISABLE_IPV6=false', '-v', '/data', '-v', '/etc/letsencrypt',
                '-v', f'{self.root}:/fixture:ro', IMAGE)
        mounts = json.loads(command('docker', 'inspect', '--format', '{{json .Mounts}}', self.name).stdout)
        owned_volumes = [m['Name'] for m in mounts if m['Type'] == 'volume']
        try:
            evidence = {'image': IMAGE, 'templates': {}, 'native': {}, 'adapter': {}}
            for path in TEMPLATES:
                content = command('docker', 'exec', self.name, 'cat', path).stdout
                evidence['templates'][path] = hashlib.sha256(content.encode()).hexdigest()
                (self.output / 'templates').mkdir(exist_ok=True)
                (self.output / 'templates' / pathlib.Path(path).name).write_text(content)
            evidence['nginx_build'] = command('docker', 'exec', self.name, 'nginx', '-V').stderr
            (self.output / 'nginx.conf').write_text(command('docker', 'exec', self.name, 'cat', '/etc/nginx/nginx.conf').stdout)
            for attempt in range(90):
                try:
                    token = self.api('POST', '/tokens', {'identity': 'fixture@example.test', 'secret': 'Disposable-fixture-Only123!'})['token']
                    break
                except RuntimeError:
                    if attempt == 89:
                        raise
                    time.sleep(1)
            command('docker', 'exec', self.name, 'openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                    '-keyout', '/data/hh-fixture.key', '-out', '/data/hh-fixture.crt', '-days', '1', '-subj', '/CN=pool.test')
            command('docker', 'exec', '-d', self.name, 'node', '/fixture/backends.cjs')
            self.write('/data/nginx/custom/http_top.conf', 'upstream hh_upstream_1 { zone hh_http_1 64k; server 127.0.0.1:18080; server [::1]:18081; }\n')
            self.write('/data/nginx/custom/stream.conf', 'upstream hh_upstream_2 { zone hh_stream_2 64k; server 127.0.0.1:19090; server [::1]:19091; }\n')
            proxy = dict(domain_names=['pool.test'], forward_scheme='http', forward_host='hh_upstream_1', forward_port=80,
                         caching_enabled=False, block_exploits=False, allow_websocket_upgrade=True, access_list_id=0,
                         certificate_id=0, meta={}, advanced_config='', locations=[dict(path='/custom/', forward_scheme='http', forward_host='hh_upstream_1', forward_port=80)],
                         http2_support=False, hsts_enabled=False, hsts_subdomains=False, ssl_forced=False)
            stream = dict(incoming_port=16060, forwarding_host='hh_upstream_2', forwarding_port=80,
                          tcp_forwarding=True, udp_forwarding=True, certificate_id=0, meta={})
            p = self.api('POST', '/nginx/proxy-hosts', proxy, token)
            s = self.api('POST', '/nginx/streams', stream, token)
            evidence['native']['proxy_meta'] = p.get('meta')
            evidence['native']['stream_meta'] = s.get('meta')
            for protocol, path in [('http', '/'), ('http', '/custom/'), ('tcp', '/'), ('udp', '/')]:
                key = protocol + path
                try:
                    evidence['native'][key] = self.probe(protocol, path)
                except RuntimeError:
                    evidence['native'][key] = 'failed'
            # Prototype: omit the member port only for reserved managed names.
            for path in TEMPLATES:
                content = (self.output / 'templates' / pathlib.Path(path).name).read_text()
                if path.endswith('proxy.conf'):
                    # The common include keeps a single proxy_pass and works for direct hosts too.
                    content = content.replace('proxy_pass       $forward_scheme://$server:$port$request_uri;',
                        'set $hh_target "$server:$port";\nif ($server ~ "^hh_upstream_[0-9]+$") { set $hh_target $server; }\nproxy_pass $forward_scheme://$hh_target$request_uri;')
                elif path.endswith('_location.conf'):
                    content = content.replace('{{ forward_host }}:{{ forward_port }}', '{{ forward_host }}{% assign hh_prefix = forward_host | slice: 0, 12 %}{% unless hh_prefix == "hh_upstream_" %}:{{ forward_port }}{% endunless %}')
                elif path.endswith('stream.conf'):
                    content = content.replace('{{ forwarding_host }}:{{ forwarding_port }}', '{{ forwarding_host }}{% assign hh_prefix = forwarding_host | slice: 0, 12 %}{% unless hh_prefix == "hh_upstream_" %}:{{ forwarding_port }}{% endunless %}')
                self.write(path, content)
            self.api('PUT', f"/nginx/proxy-hosts/{p['id']}", proxy, token)
            self.api('PUT', f"/nginx/streams/{s['id']}", stream, token)
            for kind, identifier in [('proxy_host', p['id']), ('stream', s['id'])]:
                (self.output / f'{kind}.conf').write_text(command('docker', 'exec', self.name, 'sh', '-c', f'cat /data/nginx/{kind}/{identifier}.conf*', check=False).stdout)
            command('docker', 'exec', self.name, 'nginx', '-t')
            self.write('/data/nginx/custom/http_top.conf', '# existing user HTTP configuration\n')
            self.write('/data/nginx/custom/stream.conf', '# existing user stream configuration\n')
            # Restore pristine templates so production installs its own exact adapter.
            for template_path in TEMPLATES:
                self.write(template_path, (self.output / 'templates' / pathlib.Path(template_path).name).read_text())
            # Exercise the production renderer, pinned adapter and activation probe.
            env = dict(os.environ, HYPERHIVE_TEST_NPM_CONTAINER=self.name)
            result = subprocess.run(['go', 'test', './npm', '-run', '^TestUpstreamDockerRuntime$', '-v', '-count=1'],
                                    cwd=self.root.parents[1], env=env, text=True, capture_output=True, timeout=300)
            (self.output / 'runtime-tests.log').write_text(result.stdout + result.stderr)
            if result.returncode:
                raise RuntimeError('production runtime verification failed: ' + result.stdout[-3000:] + result.stderr[-3000:])
            evidence['production_runtime'] = 'passed; seven HTTP methods and six stream methods validated against exact image'
            time.sleep(1)
            for protocol, path in [('http', '/'), ('http', '/custom/'), ('tcp', '/'), ('udp', '/')]:
                labels = {self.probe(protocol, path) for _ in range(12)}
                evidence['adapter'][protocol + path] = sorted(labels)
                if labels != {'member-1', 'member-2'}:
                    raise RuntimeError(f'{protocol}{path} did not reach both members: {labels}')
            evidence['result'] = 'prototype-traffic-passed'
            self.api('DELETE', f"/nginx/proxy-hosts/{p['id']}", token=token)
            self.api('DELETE', f"/nginx/streams/{s['id']}", token=token)
            env['HYPERHIVE_TEST_NPM_TOKEN'] = token
            result = subprocess.run(['go', 'test', './npm', '-run', '^TestUpstreamDockerLifecycle$', '-v', '-count=1'],
                                    cwd=self.root.parents[1], env=env, text=True, capture_output=True, timeout=300)
            (self.output / 'lifecycle-tests.log').write_text(result.stdout + result.stderr)
            if result.returncode:
                raise RuntimeError('upstream lifecycle verification failed: ' + result.stdout[-4000:] + result.stderr[-1000:])
            evidence['lifecycle'] = 'passed'
            result = subprocess.run(['go', 'test', './npm', '-run', '^TestUpstreamDockerRestart$', '-v', '-count=1'],
                                    cwd=self.root.parents[1], env=env, text=True, capture_output=True, timeout=120)
            (self.output / 'restart-test.log').write_text(result.stdout + result.stderr)
            if result.returncode:
                raise RuntimeError('restart verification failed: ' + result.stdout[-3000:] + result.stderr[-3000:])
            evidence['restart'] = 'passed'
            return evidence
        finally:
            (self.output / 'candidate-validation.log').write_text(command('docker', 'exec', self.name, 'nginx', '-t', '-p', '/etc/nginx/', '-c', '/etc/nginx/hyperhive-candidate-100.conf', check=False).stderr)
            (self.output / 'evidence.json').write_text(json.dumps(evidence, indent=2) + '\n')
            logs = command('docker', 'logs', self.name, check=False)
            (self.output / 'container.log').write_text(logs.stdout + logs.stderr)
            command('docker', 'rm', '-fv', self.name, check=False)
            for volume in owned_volumes:
                command('docker', 'volume', 'rm', volume, check=False)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--image', default=IMAGE)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    args = parser.parse_args()
    IMAGE = args.image
    args.output.mkdir(parents=True, exist_ok=True)
    fixture = Fixture(pathlib.Path(__file__).resolve().parent)
    fixture.output = args.output.resolve()
    evidence = fixture.run()
    (fixture.output / 'evidence.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print(json.dumps(evidence, indent=2))
