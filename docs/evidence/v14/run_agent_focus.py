import os,sys,json,tempfile,subprocess,pty,fcntl,termios,struct,time,shutil,importlib.util
from pathlib import Path
s=importlib.util.spec_from_file_location('visual',Path(__file__).resolve().parents[1]/'v11/run_visuals.py');v=importlib.util.module_from_spec(s);s.loader.exec_module(v)
plugin=Path(sys.argv[1]).resolve();out=Path(sys.argv[2]);out.mkdir(parents=True,exist_ok=True)
iso=Path(tempfile.mkdtemp(prefix='hseh-focus-repro-'));sock=iso/'.config/herdr/sessions/hseh-focus-repro/herdr.sock'
env={k:os.environ[k] for k in ('PATH','LANG') if k in os.environ};env.update(HOME=str(iso),XDG_CONFIG_HOME=str(iso/'.config'),XDG_STATE_HOME=str(iso/'.local/state'),XDG_DATA_HOME=str(iso/'.local/share'),XDG_CACHE_HOME=str(iso/'.cache'),TERM='xterm-256color',HERDR_SESSION='hseh-focus-repro',HERDR_SOCKET_PATH=str(sock))
(iso/'.config/herdr').mkdir(parents=True);(iso/'.config/herdr/config.toml').write_text('onboarding = false\n[experimental]\nallow_nested = true\n')
fd=child=server=None;acc=bytearray();before=v.base.sha256(v.base.USER_PLUGINS)
def h(*args):
 r=v.base.run(env,['herdr',*args]);assert r.returncode==0,r.stderr+r.stdout
 if args[:2]==('agent','focus'):(out/'agent-focus-response.json').write_text(r.stdout)
 return r.stdout
def j(*args):return json.loads(h(*args))['result']
def frame(name,delay=.8):
 v.base.acc_drain(fd,delay,acc);screen=v.StyledScreen();screen.feed(acc.decode('utf8','replace'));(out/(name+'.raw')).write_bytes(acc);(out/(name+'.txt')).write_text(screen.dump());return screen.dump()
def snap(name):
 d=j('api','snapshot')['snapshot'];(out/(name+'.json')).write_text(json.dumps(d,indent=2));return d
try:
 with (out/'server.log').open('w') as log:server=subprocess.Popen(['herdr','--session','hseh-focus-repro','server'],env=env,stdout=log,stderr=subprocess.STDOUT)
 for _ in range(60):
  if sock.exists():break
  time.sleep(.1)
 assert sock.exists()
 a=j('workspace','create','--label','focus-space','--cwd',str(iso));w=a['workspace']['workspace_id'];base=a['root_pane']['pane_id']
 t=j('tab','create','--workspace',w,'--label','Agent tab','--cwd',str(iso),'--no-focus');pa=t['root_pane']['pane_id'];tab=t['tab']['tab_id']
 pb=j('pane','split',pa,'--direction','right','--cwd',str(iso),'--no-focus')['pane']['pane_id']
 b=j('workspace','create','--label','other-space','--cwd',str(iso));other=b['root_pane']['pane_id']
 for pane,label in [(pa,'TargetA'),(pb,'TargetB')]:
  h('pane','report-agent',pane,'--source','custom:hseh-focus-repro','--agent','pi','--state','idle')
  h('pane','report-metadata',pane,'--source','custom:hseh-focus-repro','--display-agent','pi - '+label)
 h('plugin','link',str(plugin));h('workspace','focus',b['workspace']['workspace_id'])
 child,fd=pty.fork()
 if child==0:
  os.environ.clear();os.environ.update(env);fcntl.ioctl(1,termios.TIOCSWINSZ,struct.pack('HHHH',v.ROWS,v.COLS,0,0));os.execvp('herdr',['herdr','--session','hseh-focus-repro'])
 fcntl.ioctl(fd,termios.TIOCSWINSZ,struct.pack('HHHH',v.ROWS,v.COLS,0,0));frame('attached',1.2)
 outcomes=[]
 for case,command,initial in [('other-workspace','workspace',b['workspace']['workspace_id']),('other-tab','tab',a['tab']['tab_id']),('other-pane','agent',pa)]:
  h(command,'focus',initial)
  if command=='agent':h('tab','focus',tab)
  frame(case+'-initial');snap(case+'-before')
  h('plugin','action','invoke','hseh.agents');text=frame(case+'-popup',1.2);assert '┌hseh' in text,text
  os.write(fd,b'TargetB');text=frame(case+'-selected');assert 'TargetB' in text,text
  os.write(fd,b'\r');text=frame(case+'-enter',1.2);d=snap(case+'-after')
  marker='hseh_focus_input_probe'
  assert marker not in h('pane','read',pb,'--source','visible'), 'stale marker in target before input'
  os.write(fd,marker.encode());frame(case+'-probe',.5)
  reads={p:h('pane','read',p,'--source','visible') for p in [base,pa,pb,other]}
  (out/(case+'-probe.json')).write_text(json.dumps(reads,indent=2))
  os.write(fd,b'\x15');frame(case+'-cleared',.3)
  actual=(d.get('focused_workspace_id'),d.get('focused_tab_id'),d.get('focused_pane_id'));expected=(w,tab,pb)
  outcomes.append({'case':case,'actual':actual,'expected':expected,'popup_open':'┌hseh' in text,'input_reached_target':marker in reads[pb]})
  if actual!=expected or '┌hseh' in text or marker not in reads[pb]:
   (out/'outcomes.json').write_text(json.dumps(outcomes,indent=2));raise AssertionError(outcomes[-1])
 (out/'outcomes.json').write_text(json.dumps(outcomes,indent=2));print('PASS exact agent focus',out)
finally:
 if sock.exists():
  (out/'plugin-logs.json').write_text(h('plugin','log','list','--plugin','hseh','--limit','20'))
 if fd is not None:os.close(fd)
 if child:os.waitpid(child,0)
 if sock.exists():v.base.run(env,['herdr','server','stop'])
 if server:server.wait(timeout=8)
 shutil.rmtree(iso)
 assert v.base.sha256(v.base.USER_PLUGINS)==before
