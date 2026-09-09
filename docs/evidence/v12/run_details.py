#!/usr/bin/env python3
"""Real Git checkout + hosted Herdr rows; controlled API status fixture, no model calls."""
import importlib.util
import json
import os
from pathlib import Path
import pty
import fcntl
import struct
import subprocess
import sys
import tempfile
import termios
import shutil
import time

spec=importlib.util.spec_from_file_location('visuals',Path(__file__).resolve().parents[1]/'v11/run_visuals.py')
v=importlib.util.module_from_spec(spec);spec.loader.exec_module(v)

def main():
    plugin=Path(sys.argv[1]).resolve();out=Path(sys.argv[2]).resolve();out.mkdir(parents=True,exist_ok=True)
    iso=Path(tempfile.mkdtemp(prefix='hseh-details-'));sock=iso/'.config/herdr/sessions/hseh-details/herdr.sock'
    env={k:os.environ[k] for k in ('PATH','LANG') if k in os.environ}
    env.update(HOME=str(iso),XDG_CONFIG_HOME=str(iso/'.config'),XDG_STATE_HOME=str(iso/'.local/state'),XDG_DATA_HOME=str(iso/'.local/share'),XDG_CACHE_HOME=str(iso/'.cache'),TERM='xterm-256color',HERDR_SESSION='hseh-details',HERDR_SOCKET_PATH=str(sock))
    (iso/'.config/herdr').mkdir(parents=True)
    (iso/'.config/herdr/config.toml').write_text('onboarding = false\n[experimental]\nallow_nested = true\n[theme]\nname = "terminal"\n[ui.sidebar.agents]\nrows = [["state_icon", "workspace", "tab"], ["agent"], ["$name2"]]\n')
    before=v.base.sha256(v.base.USER_PLUGINS);fd=child=server=None
    def herdr(*args):
        r=v.base.run(env,['herdr',*args]);assert r.returncode==0,r.stderr+r.stdout
        return r.stdout
    repo=iso/'repo';repo.mkdir()
    def git(*args):
        r=v.base.run(env,['git','-C',str(repo),'-c','core.hooksPath=/dev/null','-c','commit.gpgsign=false','-c','user.name=Fixture','-c','user.email=fixture@example.invalid',*args]);assert r.returncode==0,r.stderr+r.stdout
    try:
        git('init','-b','main');(repo/'tracked').write_text('one');git('add','tracked');git('commit','-m','initial')
        (repo/'tracked').write_text('changed');(repo/'staged').write_text('new');git('add','staged');(repo/'untracked').write_text('new')
        with (iso/'server.log').open('w') as log:
            server=subprocess.Popen(['herdr','--session','hseh-details','server'],env=env,stdout=log,stderr=subprocess.STDOUT)
        for _ in range(60):
            if sock.exists():break
            time.sleep(.1)
        assert sock.exists()
        a=json.loads(herdr('workspace','create','--label','repo-space','--cwd',str(repo)))['result']
        b=json.loads(herdr('workspace','create','--label','plain-space','--cwd',str(iso)))['result']
        pane=a['root_pane']['pane_id'];tab=a['tab']['tab_id'];repo_ws=a['workspace']['workspace_id'];plain_ws=b['workspace']['workspace_id']
        herdr('tab','rename',tab,'Parser tab')
        herdr('pane','report-agent',pane,'--source','custom:hseh-details','--agent','pi','--state','working')
        herdr('pane','report-metadata',pane,'--source','custom:hseh-details','--display-agent','pi - Fix parser','--token','name2=keep important details')
        herdr('plugin','link',str(plugin))
        cfg=Path(herdr('plugin','config-dir','hseh').strip());cfg.mkdir(parents=True,exist_ok=True)
        (cfg/'hseh.toml').write_text('wide_preview_min_columns = 40\n')
        herdr('workspace','focus',plain_ws)
        child,fd=pty.fork()
        if child==0:
            os.environ.clear();os.environ.update(env)
            fcntl.ioctl(1,termios.TIOCSWINSZ,struct.pack('HHHH',v.ROWS,v.COLS,0,0))
            os.execvp('herdr',['herdr','--session','hseh-details'])
        fcntl.ioctl(fd,termios.TIOCSWINSZ,struct.pack('HHHH',v.ROWS,v.COLS,0,0))
        acc=bytearray();v.base.acc_drain(fd,1.2,acc)
        herdr('plugin','action','invoke','hseh.spaces')
        def capture(name,delay=1.2):
            v.base.acc_drain(fd,delay,acc);s=v.StyledScreen();s.feed(acc.decode('utf8','replace'))
            (out/(name+'.raw')).write_bytes(acc);(out/(name+'.txt')).write_text(s.dump());s.png(out/(name+'.png'))
            return s
        s=capture('spaces',2)
        origin=next(s.row(r).index('┌hseh') for r in range(v.ROWS) if '┌hseh' in s.row(r))
        def popup_hit(s,label):
            for r in range(v.ROWS):
                c=s.row(r).find(label,origin+1)
                if c>=0:return r,c
            raise AssertionError(label+' missing\n'+s.dump())
        r,c=popup_hit(s,'repo-space')
        assert 'main' in s.row(r) and '+1 ~1 ?1' in s.row(r),s.dump()
        assert s.bold_cells[r][c], 'space name not bold'
        assert s.backgrounds[r][c]==v.GRAY, 'selected row lost background'
        git_col=s.row(r).index('main',c)
        assert s.foregrounds[r][git_col]!=s.foregrounds[r][c], 'git info not gray'
        def status_colors(screen,glyph):
            rr,cc=popup_hit(screen,'repo-space')
            dot=screen.row(rr).rfind(glyph,origin,cc);assert dot>=0,screen.dump()
            native=next((nr,screen.row(nr).find(glyph,0,origin)) for nr in range(v.ROWS) if screen.row(nr).find('repo-space',0,origin)>=0 and screen.row(nr).find(glyph,0,origin)>=0)
            native_color=screen.foregrounds[native[0]][native[1]];hseh_color=screen.foregrounds[rr][dot]
            assert native_color==hseh_color, (native_color,hseh_color)
            return {'herdr':native_color,'hseh':hseh_color,'glyph':glyph}
        colors={'working':status_colors(s,'●')}
        for reported,effective in [('blocked','blocked'),('idle','done')]:
            herdr('pane','report-agent',pane,'--source','custom:hseh-details','--agent','pi','--state',reported)
            colors[effective]=status_colors(capture(effective),'●')
        os.write(fd,b'\x1b')
        capture('done-popup-closed')
        herdr('workspace','focus',repo_ws)
        capture('idle-focused')
        herdr('plugin','action','invoke','hseh.spaces')
        colors['idle']=status_colors(capture('idle'),'○')
        os.write(fd,b'\x1b')
        capture('idle-popup-closed')
        herdr('pane','report-agent',pane,'--source','custom:hseh-details','--agent','pi','--state','working')
        herdr('workspace','focus',plain_ws)
        capture('plain-focused')
        herdr('plugin','action','invoke','hseh.spaces')
        capture('working-again')
        (out/'status-colors.json').write_text(json.dumps(colors,indent=2))
        (repo/'another').write_text('new');updated=capture('git-updated')
        assert '+1 ~1 ?2' in updated.dump(), updated.dump()
        os.write(fd,b'\t');s=capture('agents')
        r,c=popup_hit(s,'\U000f03ff Parser tab');assert r>=0
        assert 'repo-space (~/repo)' in s.row(r+1),s.dump()
        assert 'pi - Fix parser keep important details' in s.row(r+2),s.dump()
        assert s.foregrounds[r+1][c]==s.foregrounds[r+2][c] and s.foregrounds[r+1][c]!=s.foregrounds[r][c], 'secondary rows not muted'
        data=json.loads(herdr('workspace','list'));assert v.base.focused_ids(data)==[plain_ws]
        os.write(fd,b'\r');capture('entered')
        assert v.base.focused_ids(json.loads(herdr('workspace','list')))==[repo_ws]
        (out/'notes.txt').write_text('PASS: Git counts refresh; bold space name; muted Git/detail rows; colored status equals native Herdr terminal theme; three agent rows; Enter focus\nBinary '+v.base.sha256(plugin/'hseh')+'\nControlled status via official API; no model turn or done/seen claim.\n')
        print('PASS details e2e',out)
    except BaseException:
        if sock.exists():
            (out/'failure-plugin-logs.json').write_text(herdr('plugin','log','list','--plugin','hseh','--limit','20'))
        raise
    finally:
        if fd is not None:os.close(fd)
        if child:os.waitpid(child,0)
        if sock.exists():v.base.run(env,['herdr','server','stop'])
        if server:server.wait(timeout=8)
        shutil.rmtree(iso)
        assert v.base.sha256(v.base.USER_PLUGINS)==before

if __name__=='__main__':main()
