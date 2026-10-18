#!/usr/bin/env python3
"""Disposable real v0.6.2 probe. No fleet credentials or privileged agents.
CELLD_TEST_BINARY=/path/celld PATH=/path/esbuild:$PATH python3 scripts/probe_celld_observability.py OUTPUT_DIR
Optional CELLD_PROBE_PROJECT selects another disposable fixture (e.g. Python/container).
"""
import http.server, json, os, pathlib, re, shutil, signal, socket, subprocess, sys, tempfile, threading, time, urllib.request, urllib.error
out = pathlib.Path(sys.argv[1]).resolve(); out.mkdir(parents=True, exist_ok=True)
binary = os.environ["CELLD_TEST_BINARY"]
fixture = pathlib.Path(os.environ.get("CELLD_PROBE_PROJECT", str(pathlib.Path(__file__).resolve().parents[1] / "slave/celld/testdata/observability")))
class Collector(http.server.BaseHTTPRequestHandler):
    count = 0
    def do_POST(self):
        size = int(self.headers.get('Content-Length', 0))
        if size > 2**20: self.send_error(413); return
        data = self.rfile.read(size)
        Collector.count += 1
        (out / (self.path.rsplit('/',1)[-1] + '-' + str(Collector.count) + '.pb')).write_bytes(data)
        self.send_response(200); self.send_header('Content-Type','application/x-protobuf'); self.end_headers()
    def log_message(self,*args): pass
server = http.server.ThreadingHTTPServer(('127.0.0.1',0), Collector)
threading.Thread(target=server.serve_forever,daemon=True).start()
with tempfile.TemporaryDirectory(prefix='celld-probe-') as tmp:
    project = pathlib.Path(tmp)/'project'; shutil.copytree(fixture,project,ignore=shutil.ignore_patterns('.celld','node_modules'))
    if (project/'package.json').exists(): subprocess.run(['npm','install','--prefix',str(project),'--no-audit','--no-fund'],check=True)
    with socket.socket() as sock: sock.bind(('127.0.0.1',0)); port=sock.getsockname()[1]
    env = dict(os.environ, CELLD_TOKIO_THREADS='2', CELLD_OTEL=f'http://127.0.0.1:{server.server_port}', CELLD_OTEL_FLUSH_MS='1000', OTEL_TRACES_SAMPLER='always_on', NO_COLOR='1')
    log = open(out/'process.log','w+')
    proc=subprocess.Popen([binary,'dev',str(project),'--port',str(port),'--no-watch','--logs'],env=env,stdout=log,stderr=log,start_new_session=True)
    try:
        internal = None
        for _ in range(180):
            time.sleep(.5); log.flush(); text=(out/'process.log').read_text()
            match=re.search(r'celld internal listening on ([^ ]+)',text)
            if match and 'ready  http' in text: internal='http://'+match[1]; break
            if proc.poll() is not None: raise RuntimeError(text[-6000:])
        if not internal: raise RuntimeError('probe failed to start')
        def get(url):
            try:
                with urllib.request.urlopen(url,timeout=10) as r: return r.status,r.read().decode()
            except urllib.error.HTTPError as e: return e.code,e.read().decode()
        states={}; responses={}
        states['idle']=json.loads(get(internal+'/state')[1])
        paths=os.environ.get('CELLD_PROBE_PATHS','/,/cell,/queue,/workflow,/workflow-failure,/bindings,/dynamic,/facet,/failure').split(',')
        for path in paths:
            responses[path]=get(f'http://127.0.0.1:{port}'+path)
            time.sleep(.3)
            states[path]=json.loads(get(internal+'/state')[1])
        time.sleep(float(os.environ.get('CELLD_PROBE_WAIT','6')))
        states['settled']=json.loads(get(internal+'/state')[1])
        (out/'snapshots.json').write_text(json.dumps(states,indent=2))
        (out/'responses.json').write_text(json.dumps(responses,indent=2))
        print(json.dumps({'output':str(out),'responses':{k:v[0] for k,v in responses.items()},'otlp_batches':Collector.count}))
        if any(v[0]>=400 for k,v in responses.items() if k!='/failure'): raise RuntimeError('fixture request failed; inspect responses.json')
    finally:
        os.killpg(proc.pid,signal.SIGTERM)
        try: proc.wait(timeout=10)
        except subprocess.TimeoutExpired: os.killpg(proc.pid,signal.SIGKILL); proc.wait()
        log.close(); server.shutdown()
