from pathlib import Path
import json, wave
P=Path(__file__).resolve().parents[1]
D=10
base='''
@font-face{font-family:Inter;src:url('assets/fonts/Inter-Variable.ttf');font-weight:100 900}
*{box-sizing:border-box}#root{position:absolute;inset:0;font-family:Inter,sans-serif;overflow:hidden;color:#E8EBF0}
.clip{position:absolute;inset:0}.ground{position:absolute;inset:0;background:#0D1420}
h1,h2,p{margin:0}h1{font-size:104px;font-weight:700;letter-spacing:-5px;line-height:1.07}h2{font-size:64px;letter-spacing:-2px;line-height:1.12}.body{font-size:36px;line-height:1.4}.small{font-size:30px;line-height:1.4}.accent{color:#28B8DA}.label{font-size:34px;font-weight:600}.hairline{height:2px;background:currentColor;opacity:.22}
'''
def write(n,name,html,css,js):
    sid=f's{n}-{name}'
    doc=f'''<template><style>{base}{css}</style>
<div id="root" data-composition-id="{sid}" data-duration="10.5" data-width="1920" data-height="1080">
<div class="clip" id="s{n}-stage" data-start="0" data-duration="10.5" data-track-index="0"><div class="ground"></div>{html}</div></div>
<script>(function(){{const tl=gsap.timeline({{paused:true}});{js}window.__timelines["{sid}"]=tl;}})();</script></template>'''
    (P/f'compositions/{sid}.html').write_text(doc)
    return sid
ids=[]
# 1. Headline is immediately present; cluster builds below it, revealing the control plane.
html='''<h1 id="s1-title" style="position:absolute;left:145px;top:125px">Your infrastructure.</h1>
<h1 id="s1-together" class="accent" style="position:absolute;left:140px;top:245px;font-size:160px">Together.</h1>
<svg style="position:absolute;inset:0;width:1920px;height:1080px" viewBox="0 0 1920 1080" aria-hidden="true">
<g id="s1-links" fill="none" stroke="#28B8DA" stroke-width="4"><path d="M440 693H960V570"/><path d="M960 570V693H1480"/></g></svg>
<div id="s1-master" class="master"><span>HyperHive</span><b>Control plane</b></div>
<div id="s1-host1" class="host" style="left:185px"><div class="rack"><i></i><i></i><i></i></div><span>Compute</span></div>
<div id="s1-host2" class="host" style="left:705px"><div class="rack"><i></i><i></i><i></i></div><span>Storage</span></div>
<div id="s1-host3" class="host" style="left:1225px"><div class="rack"><i></i><i></i><i></i></div><span>Networking</span></div>
<p id="s1-foot" class="body" style="position:absolute;left:145px;top:930px">One control plane. Your own infrastructure.</p>'''
css='''.master{position:absolute;left:685px;top:450px;width:550px;height:130px;background:#28B8DA;color:#0D1420;display:flex;justify-content:center;flex-direction:column;align-items:center;border-radius:10px}.master span{font-size:48px;font-weight:700}.master b{font-size:30px;font-weight:400}.host{position:absolute;top:640px;width:510px;text-align:center}.host span{display:block;font-size:38px;margin-top:25px}.rack{padding:22px 36px;border:2px solid #52667B;border-radius:10px;background:#152334}.rack i{display:block;height:24px;margin:12px 0;background:#2A3F53;border-right:50px solid #28B8DA;border-radius:4px}'''
js='''tl.fromTo('#s1-together',{clipPath:'inset(0 100% 0 0)'},{clipPath:'inset(0 0% 0 0)',duration:.8,ease:'expo.out'},.2);
tl.fromTo('#s1-master',{scale:.8,opacity:0},{scale:1,opacity:1,duration:.6,ease:'power4.out'},2);
tl.fromTo('#s1-links',{opacity:0},{opacity:1,duration:.5},3);
['#s1-host1','#s1-host2','#s1-host3'].forEach((s,i)=>tl.fromTo(s,{y:80,opacity:0},{y:0,opacity:1,duration:.65,ease:'power4.out'},3.1+i*.85));
tl.fromTo('#s1-foot',{opacity:0},{opacity:1,duration:.4},6.6);
'''
ids.append(write(1,'together',html,css,js))
# 2. VMs live on recognisable rack bays; one VM is copied, then moves between hosts.
html='''<h1 id="s2-title" style="position:absolute;left:140px;top:130px">Compute, on your terms.</h1>
<p class="body" id="s2-sub" style="position:absolute;left:145px;top:270px">Virtual machines, from creation to migration.</p>
<div id="s2-left" class="tower" style="left:145px"><span>Host agent A</span><div class="bay"></div><div class="bay"></div><div class="bay"></div><div class="rackfoot"></div></div>
<div id="s2-right" class="tower" style="left:1125px"><span>Host agent B</span><div class="bay"></div><div class="bay"></div><div class="bay"></div><div class="rackfoot"></div></div>
<svg id="s2-arrow" style="position:absolute;inset:0;width:1920px;height:1080px" viewBox="0 0 1920 1080" aria-hidden="true"><path d="M690 570H1200M1165 540L1200 570L1165 600" fill="none" stroke="#0D1420" stroke-width="5"/></svg>
<div id="s2-vm" class="vm" style="left:195px;top:456px">Virtual machine</div><div id="s2-clone" class="vm" style="left:195px;top:582px">Clone</div>
<p id="s2-verbs" class="body" style="position:absolute;left:145px;top:930px;font-weight:600">Create. Clone. Back up. Migrate.</p>'''
css='''.ground{background:#28B8DA}#root{color:#0D1420}.tower{position:absolute;top:375px;width:650px;height:470px;background:#0D1420;border-radius:12px;padding:28px 32px}.tower>span{color:#E8EBF0;display:block;font-size:32px;margin-bottom:20px}.bay{height:112px;border:2px solid #52667B;border-radius:6px;margin-bottom:12px}.rackfoot{height:14px;width:80%;background:#0D1420;position:absolute;bottom:-14px;left:10%}.vm{position:absolute;width:550px;height:92px;display:flex;align-items:center;padding-left:28px;color:#0D1420;background:#E8EBF0;border-left:12px solid #0A7EA4;border-radius:5px;font-size:36px;font-weight:600}'''
js='''tl.fromTo('#s2-stage',{x:1920},{x:0,duration:.5,ease:'power4.out'},0);
tl.fromTo('#s2-left',{y:65,opacity:0},{y:0,opacity:1,duration:.65,ease:'expo.out'},.7);
tl.fromTo('#s2-right',{y:65,opacity:0},{y:0,opacity:1,duration:.65,ease:'expo.out'},1.1);
tl.fromTo('#s2-vm',{scaleX:0,transformOrigin:'left center'},{scaleX:1,duration:.5,ease:'expo.out'},2);
tl.fromTo('#s2-clone',{opacity:0,y:-126},{opacity:1,y:0,duration:.65,ease:'power2.inOut'},3.8);
tl.fromTo('#s2-arrow',{opacity:0},{opacity:1,duration:.25},5.4);
tl.to('#s2-vm',{x:980,duration:1.25,ease:'power2.inOut'},5.7);
tl.fromTo('#s2-verbs',{opacity:0},{opacity:1,duration:.4},7.2);
'''
ids.append(write(2,'compute',html,css,js))
# 3. Storage as a stack of discs shared across two explicit source-backed technologies.
html='''<h1 id="s3-title" style="position:absolute;left:140px;top:120px">Give every workload<br>solid ground.</h1>
<div id="s3-pool" class="pool"><div class="disk d3"></div><div class="disk d2"></div><div class="disk d1"></div><div class="disk top"><span>Storage</span></div></div>
<div id="s3-btrfs" class="storage-label" style="left:155px;top:500px"><h2>Btrfs</h2><p class="body">Filesystem management</p></div>
<div id="s3-nfs" class="storage-label" style="left:1240px;top:500px"><h2>NFS</h2><p class="body">Shared storage</p></div>
<svg id="s3-wires" style="position:absolute;inset:0;width:1920px;height:1080px" viewBox="0 0 1920 1080" aria-hidden="true"><path d="M610 610H735M1185 610H1210" fill="none" stroke="#0A7EA4" stroke-width="5"/></svg>
<div id="s3-health" class="health"><span class="dot"></span><span>Disk health &amp; backups</span></div>
<p id="s3-foot" class="body" style="position:absolute;left:145px;top:930px">Storage and compute, in the same picture.</p>'''
css='''.ground{background:#E5F2F6}#root{color:#0D1420}.pool{position:absolute;left:735px;top:420px;width:450px;height:380px}.disk{position:absolute;width:450px;height:180px;border-radius:50%;background:#C2D8E1;border:4px solid #0A7EA4}.d3{top:180px}.d2{top:120px}.d1{top:60px}.top{top:0;background:#E5F2F6;display:flex;align-items:center;justify-content:center}.top span{font-size:52px;font-weight:600}.health{position:absolute;left:680px;top:845px;display:flex;gap:20px;align-items:center;font-size:34px}.dot{display:block;width:18px;height:18px;background:#0A7EA4;border-radius:50%}.storage-label{position:absolute;width:520px}.storage-label p{margin-top:20px}'''
js='''tl.fromTo('#s3-stage',{y:1080},{y:0,duration:.5,ease:'power4.out'},0);
tl.fromTo('#s3-pool',{scaleY:.1,opacity:0,transformOrigin:'center bottom'},{scaleY:1,opacity:1,duration:.8,ease:'expo.out'},1.2);
tl.fromTo('#s3-btrfs',{x:-50,opacity:0},{x:0,opacity:1,duration:.6,ease:'power4.out'},2.4);
tl.fromTo('#s3-nfs',{x:50,opacity:0},{x:0,opacity:1,duration:.6,ease:'power4.out'},4);
tl.fromTo('#s3-wires',{opacity:0},{opacity:1,duration:.4},5.2);
tl.fromTo('#s3-health',{opacity:0},{opacity:1,duration:.4},6.7);
tl.fromTo('#s3-foot',{opacity:0},{opacity:1,duration:.4},7.7);
'''
ids.append(write(3,'storage',html,css,js))
# 4. Routes use no invented live addresses or application UI.
html='''<h1 id="s4-title" style="position:absolute;left:140px;top:125px">Connect your world.</h1>
<p id="s4-sub" class="body" style="position:absolute;left:145px;top:270px">From the edge to your services.</p>
<svg style="position:absolute;inset:0;width:1920px;height:1080px" viewBox="0 0 1920 1080" aria-hidden="true"><path id="s4-path" d="M335 550H1585" stroke="#28B8DA" stroke-width="5" fill="none"/><circle id="s4-packet" cx="335" cy="550" r="15" fill="#E8EBF0"/></svg>
<div id="s4-edge" class="node" style="left:145px"><div class="symbol globe"><i></i></div><h2>Edge</h2></div>
<div id="s4-proxy" class="node" style="left:770px"><div class="symbol proxy"><i></i><i></i><i></i></div><h2>Proxy</h2></div>
<div id="s4-service" class="node" style="left:1395px"><div class="symbol service"><i></i><i></i><i></i></div><h2>Service</h2></div>
<div id="s4-details" style="position:absolute;left:145px;top:820px;display:flex;gap:80px"><p class="body">Certificates</p><p class="body">DNS</p><p class="body">WireGuard</p></div>
<div id="s4-rule" class="hairline" style="position:absolute;left:145px;top:895px;width:1630px"></div>
<p id="s4-foot" class="small" style="position:absolute;left:145px;top:940px">Networking, alongside the machines it serves.</p>'''
css='''.node{position:absolute;top:400px;width:380px;text-align:center}.symbol{width:300px;height:300px;display:flex;flex-direction:column;justify-content:center;align-items:center;gap:20px;margin:0 auto 40px;background:#0D1420;border:4px solid #28B8DA}.globe{border-radius:50%;position:relative}.globe:before{content:'';position:absolute;inset:25px 80px;border:3px solid #28B8DA;border-radius:50%}.globe i{width:290px;height:100px;border-top:3px solid #28B8DA;border-bottom:3px solid #28B8DA;border-radius:50%}.proxy{border-radius:26px}.proxy i{width:160px;height:10px;background:#28B8DA}.service{border-radius:12px}.service i{height:45px;width:215px;border:2px solid #52667B;border-right:28px solid #28B8DA}'''
js='''tl.fromTo('#s4-title',{opacity:0},{opacity:1,duration:.3},0);
['#s4-edge','#s4-proxy','#s4-service'].forEach((s,i)=>tl.fromTo(s,{scale:.8,opacity:0},{scale:1,opacity:1,duration:.65,ease:'power4.out'},.5+i*1.2));
tl.fromTo('#s4-path',{strokeDasharray:1250,strokeDashoffset:1250},{strokeDashoffset:0,duration:1.5,ease:'power2.inOut'},1.6);
tl.fromTo('#s4-packet',{opacity:0},{opacity:1,duration:.2},3.5);tl.to('#s4-packet',{x:1250,duration:1.5,ease:'power2.inOut'},3.6);
tl.fromTo('#s4-details',{opacity:0,y:25},{opacity:1,y:0,duration:.6,ease:'expo.out'},5.7);
tl.fromTo('#s4-foot',{opacity:0},{opacity:1,duration:.4},7.5);
'''
ids.append(write(4,'network',html,css,js))
# 5. Real MCP tool names shown as a conceptual contract rather than fake output.
html='''<h1 id="s5-title" style="position:absolute;left:140px;top:120px">Your tools.<br>Your infrastructure.</h1>
<div id="s5-client" class="mcp-node" style="left:145px;top:470px;width:390px"><span>AI client</span></div>
<div id="s5-mcp" class="mcp-node central" style="left:690px;top:445px;width:540px;height:220px"><span>HyperHive</span><b>MCP</b></div>
<div id="s5-resources" class="mcp-node" style="left:1380px;top:470px;width:390px"><span>Resources</span></div>
<svg id="s5-links" style="position:absolute;inset:0;width:1920px;height:1080px" viewBox="0 0 1920 1080" aria-hidden="true"><path d="M535 550H690M1230 550H1380" fill="none" stroke="#0A7EA4" stroke-width="5"/></svg>
<div id="s5-tool" class="tool"><span class="small">Supported tool</span><code id="s5-tool1">list_proxies</code><code id="s5-tool2">list_certificates</code><code id="s5-tool3">get_celld_health</code></div>
<p id="s5-foot" class="body" style="position:absolute;left:145px;top:930px">Explicit tools. Defined operations.</p>'''
css='''.ground{background:#E5F2F6}#root{color:#0D1420}.mcp-node{position:absolute;height:170px;border:3px solid #0A7EA4;border-radius:12px;display:flex;align-items:center;justify-content:center;font-size:42px;font-weight:600;background:#E5F2F6}.central{background:#0D1420;color:#E8EBF0;flex-direction:column}.central span{font-size:48px}.central b{font-size:60px;color:#28B8DA;letter-spacing:6px;margin-top:10px}.tool{position:absolute;left:145px;top:750px;width:1630px;height:125px;border-top:2px solid #94B7C5;display:flex;align-items:center;gap:50px}.tool>span{width:290px}.tool code{position:absolute;left:340px;font-family:Inter,sans-serif;font-size:52px;font-weight:600;letter-spacing:-1px;color:#0A7EA4}'''
js='''tl.fromTo('#s5-stage',{x:-1920},{x:0,duration:.5,ease:'power4.out'},0);
tl.fromTo('#s5-client',{opacity:0,x:-40},{opacity:1,x:0,duration:.6,ease:'expo.out'},.7);
tl.fromTo('#s5-mcp',{scale:.8,opacity:0},{scale:1,opacity:1,duration:.65,ease:'expo.out'},1.8);
tl.fromTo('#s5-resources',{opacity:0,x:40},{opacity:1,x:0,duration:.6,ease:'expo.out'},2.7);
tl.fromTo('#s5-links',{opacity:0},{opacity:1,duration:.3},3.2);
tl.fromTo('#s5-tool',{opacity:0},{opacity:1,duration:.35},3.7);
tl.fromTo('#s5-tool1',{opacity:0},{opacity:1,duration:.15},3.7);tl.to('#s5-tool1',{opacity:0,duration:.15},5.3);
tl.fromTo('#s5-tool2',{opacity:0},{opacity:1,duration:.15},5.5);tl.to('#s5-tool2',{opacity:0,duration:.15},7.1);
tl.fromTo('#s5-tool3',{opacity:0},{opacity:1,duration:.15},7.3);
tl.fromTo('#s5-foot',{opacity:0},{opacity:1,duration:.4},7.7);
'''
ids.append(write(5,'mcp',html,css,js))
# 6. A real mark, no invented logo.
html='''<div id="s6-master" class="arch" style="left:140px;top:145px;width:700px"><h2>One master.</h2><p class="body" style="margin-top:22px">A coordinated control plane.</p></div>
<div id="s6-agents" class="arch" style="left:1060px;top:145px;width:720px"><h2>Work at the hosts.</h2><p class="body" style="margin-top:22px">Agents execute local operations.</p></div>
<div id="s6-lockup" style="position:absolute;left:0;top:420px;width:1920px;display:flex;align-items:center;justify-content:center;gap:42px"><img src="assets/hyperhive-icon.png" width="155" height="155" alt="HyperHive H mark"/><h1 style="font-size:146px;letter-spacing:-7px">HyperHive</h1></div>
<p id="s6-tagline" style="position:absolute;left:0;top:680px;width:1920px;text-align:center;font-size:52px;letter-spacing:-1px">Your infrastructure, one control plane.</p>
<p id="s6-audience" class="body" style="position:absolute;left:0;top:790px;width:1920px;text-align:center">Built for homelabs and small datacenters.</p>
<p id="s6-credit" style="position:absolute;left:145px;top:965px;font-size:28px;width:1630px;text-align:center">Music: “Cipher” · Kevin MacLeod · incompetech.com · CC BY 4.0</p>'''
css='''.ground{background:#0A7EA4}.arch{position:absolute}#s6-lockup img{object-fit:contain}.arch p{color:#E8EBF0}'''
js='''tl.fromTo('#s6-master',{opacity:0,x:-50},{opacity:1,x:0,duration:.65,ease:'expo.out'},.1);
tl.fromTo('#s6-agents',{opacity:0,x:50},{opacity:1,x:0,duration:.65,ease:'expo.out'},1.2);
tl.fromTo('#s6-lockup',{clipPath:'inset(0 100% 0 0)'},{clipPath:'inset(0 0% 0 0)',duration:1,ease:'power2.inOut'},2.7);
tl.fromTo('#s6-tagline',{opacity:0},{opacity:1,duration:.45},5);
tl.fromTo('#s6-audience',{opacity:0},{opacity:1,duration:.45},6.4);
tl.fromTo('#s6-credit',{opacity:0},{opacity:1,duration:.25},7);'''
ids.append(write(6,'identity',html,css,js))
# Root composition mounts six independent, editable scenes.
hosts='\n'.join(f'<div id="{s}" class="clip" data-composition-id="{s}" data-composition-src="compositions/{s}.html" data-start="{i*10}" data-duration="{10.5 if i in (0,1,3) else 10}" data-track-index="{i+1}" data-track-kind="graphics" data-width="1920" data-height="1080"></div>' for i,s in enumerate(ids))
audios='\n'.join(f'<audio id="voice-{i+1}" class="clip" src="assets/voice-{i+1}.wav" data-start="{i*10+.5}" data-duration="{wave.open(str(P/f'assets/voice-{i+1}.wav')).getnframes()/wave.open(str(P/f'assets/voice-{i+1}.wav')).getframerate():.4f}" data-track-index="8" data-volume="0.676" data-fade-out=".03"></audio>' for i in range(6))
audios+='\n<audio id="music" class="clip" src="assets/music-bed.wav" data-start="0" data-duration="60" data-track-index="9" data-volume="0.0811" data-fade-in="0.4" data-fade-out="2"></audio>'
for i,(t,src,vol) in enumerate([(9.5,'whoosh-short.mp3',.149),(19.5,'whoosh.mp3',.128),(39.5,'whoosh-short.mp3',.149),(52.7,'riser.mp3',.054)]):
    audios+=f'\n<audio id="fx-{i}" class="clip" src="assets/{src}" data-start="{t}" data-duration="{1.4 if src == 'riser.mp3' else 0.5747}" data-track-index="{10+i}" data-volume="{vol}"></audio>'
(P/'index.html').write_text(f'''<!doctype html><html lang="en"><head><meta charset="UTF-8"><title>HyperHive — Your infrastructure, one control plane</title><script src="https://cdn.jsdelivr.net/npm/gsap@3.14.2/dist/gsap.min.js"></script><style>*{{box-sizing:border-box}}html,body{{margin:0;width:100%;height:100%;overflow:hidden;background:#0D1420}}#root{{width:100%;height:100%;position:relative}}.clip{{position:absolute;inset:0}}</style></head><body><div id="root" data-composition-id="main" data-start="0" data-duration="60" data-width="1920" data-height="1080" data-fps="30"><div id="opening-underlay" style="position:absolute;inset:0;z-index:100;pointer-events:none;font-family:Inter,sans-serif;color:#E8EBF0"><h1 data-layout-ignore style="position:absolute;left:145px;top:125px;margin:0;font-size:104px;font-weight:700;letter-spacing:-5px;line-height:1.07">Your infrastructure.</h1></div>{hosts}{audios}</div><script>const mainTimeline=gsap.timeline({{paused:true}});mainTimeline.set('#opening-underlay',{{opacity:0}},.5);window.__timelines['main']=mainTimeline;</script></body></html>''')
lines=json.loads((P/'research/narration.json').read_text())
sb='''---\nformat: 1920x1080\nduration: 60s\nmessage: Your infrastructure, one control plane\narc: Need → Control → Compute → Storage → Network → MCP → Identity\naudience: Homelab builders and small datacenter operators\nmode: autonomous\n---\n\n# Video direction\nOne workload travels through the cluster, its shared storage and the network edge. Contrast slate diagrams with cyan compute and pale blueprint storage/MCP scenes. No invented product UI.\n'''
for i,(s,line) in enumerate(zip(ids,lines),1):
    sb+=f'\n## Frame {i} — {s.split("-",1)[1].title()}\n\n- status: animated\n- src: compositions/{s}.html\n- duration: 10s\n- scene: {s.split("-",1)[1]}\n- voiceover: {line}\n- reads: Headline (0-3s); mechanism (3-7s); benefit (7-10s)\n- sound: Narration over a restrained electronic bed, transition effect only on large moves.\n- asset_candidates: Source-backed architecture diagrams, project fonts, authentic H icon at close.\n'
(P/'STORYBOARD.md').write_text(sb)
(P/'SCRIPT.md').write_text('# HyperHive narration\n\n**Voice:** local Kokoro, bm_george\n**Direction:** Calm, measured British English.\n\n'+'\n'.join(f'## Line {i} — Frame {i}\n\n    {line}\n' for i,line in enumerate(lines,1)))
print('Wrote six scenes, root timeline, script and storyboard.')
