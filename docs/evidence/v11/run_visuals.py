#!/usr/bin/env python3
"""Actual hosted UI: gray selection, active tabs, bottom-aligned live grid, mouse/Enter.
Uses the prior isolated terminal driver helpers. Optional PNGs render captured cells, not mock data.
"""
import importlib.util
import json
import os
from pathlib import Path
import pty
import fcntl
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
spec = importlib.util.spec_from_file_location('mouse_helpers', HERE.parent / 'v9/run_mouse_width.py')
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
ROWS, COLS = 40, 140
GRAY = (118, 118, 118)
# Active tab background: 256-colour (xterm 74) or truecolour catppuccin accent, depending on the terminal profile herdr reports.
ACTIVE = {(95, 175, 215), (137, 180, 250)}


def palette(n):
    if n >= 232:
        return (8 + (n-232)*10,) * 3
    if n >= 16:
        n -= 16
        levels = [0, 95, 135, 175, 215, 255]
        return (levels[n//36], levels[n//6 % 6], levels[n % 6])
    return [(0,0,0),(205,0,0),(0,205,0),(205,205,0),(0,0,238),(205,0,205),(0,205,205),(229,229,229),
            (127,127,127),(255,0,0),(0,255,0),(255,255,0),(92,92,255),(255,0,255),(0,255,255),(255,255,255)][n]


class StyledScreen(base.CellScreen):
    def __init__(self):
        super().__init__(ROWS, COLS)
        self.bg = None
        self.fg = None
        self.bold = False
        self.backgrounds = [[None]*COLS for _ in range(ROWS)]
        self.foregrounds = [[None]*COLS for _ in range(ROWS)]
        self.bold_cells = [[False]*COLS for _ in range(ROWS)]

    def put(self, ch):
        if 0 <= self.r < self.rows and 0 <= self.c < self.cols:
            self.backgrounds[self.r][self.c] = self.bg
            self.foregrounds[self.r][self.c] = self.fg
            self.bold_cells[self.r][self.c] = self.bold
        super().put(ch)

    def feed(self, data):
        for part in re.split(r'(\x1b\[[0-9;?]*[A-Za-z])', data):
            if part.startswith('\x1b[') and part.endswith('m'):
                values = [int(v or 0) for v in part[2:-1].split(';')]
                i = 0
                while i < len(values):
                    n = values[i]
                    if n in (0,49): self.bg = None
                    if n in (0,39): self.fg = None
                    if n in (0,22): self.bold = False
                    if n == 1: self.bold = True
                    if 30 <= n <= 37: self.fg = palette(n-30)
                    if 90 <= n <= 97: self.fg = palette(n-90+8)
                    if 40 <= n <= 47: self.bg = palette(n-40)
                    if n in (38,48) and i+1 < len(values):
                        mode = values[i+1]
                        if mode == 5 and i+2 < len(values):
                            if n == 48: self.bg = palette(values[i+2])
                            else: self.fg = palette(values[i+2])
                            i += 2
                        elif mode == 2 and i+4 < len(values):
                            if n == 48: self.bg = tuple(values[i+2:i+5])
                            else: self.fg = tuple(values[i+2:i+5])
                            i += 4
                    i += 1
            else:
                if part.startswith('\x1b[') and part.endswith('J'):
                    self.backgrounds = [[None]*COLS for _ in range(ROWS)]
                    self.foregrounds = [[None]*COLS for _ in range(ROWS)]
                    self.bold_cells = [[False]*COLS for _ in range(ROWS)]
                super().feed(part)

    def colored_label(self, label, color):
        colors = color if isinstance(color, set) else {color}
        for row in range(ROWS):
            col = self.row(row).find(label)
            if col >= 0 and all(self.backgrounds[row][c] in colors for c in range(col,col+len(label))):
                return row,col
        raise AssertionError(f'{label!r} lacks {color}\n{self.dump()}')

    def selected_label(self, label):
        # Since the rail UI, the selected item shows an accent rail at the list edge and a bold name instead of a gray block.
        for row in range(ROWS):
            text = self.row(row); col = text.find(label)
            if col >= 0 and '┃' in text[:col] and self.bold_cells[row][col]:
                return row,col
        raise AssertionError(f'{label!r} not selected\n{self.dump()}')

    def png(self, path):
        # PIL is optional and only used to view terminal evidence. It is not an app dependency.
        try:
            from PIL import Image, ImageDraw, ImageFont
        except ImportError:
            return
        font_path = subprocess.check_output(['fc-match','monospace','-f','%{file}'],text=True)
        font = ImageFont.truetype(font_path,14)
        icon_path = os.environ.get('HSEH_EVIDENCE_ICON_FONT')
        icon_font = ImageFont.truetype(icon_path,14) if icon_path else None
        image = Image.new('RGB',(COLS*9,ROWS*20),'black')
        draw = ImageDraw.Draw(image)
        glyph_fonts = {}
        for r in range(ROWS):
            for c in range(COLS):
                bg = self.backgrounds[r][c] or (0,0,0)
                draw.rectangle((c*9,r*20,c*9+8,r*20+19),fill=bg)
                fg = self.foregrounds[r][c] or (235,235,235)
                ch = self.cells[r][c]
                if ord(ch)>127 and ch not in glyph_fonts:
                    if icon_font and (0xe000 <= ord(ch) <= 0xf8ff or 0xf0000 <= ord(ch) <= 0x10fffd):
                        glyph_fonts[ch] = icon_font
                    else:
                        fallback = subprocess.check_output(['fc-match',f':charset={ord(ch):x}','-f','%{file}'],text=True)
                        glyph_fonts[ch] = ImageFont.truetype(fallback,14)
                draw.text((c*9,r*20),ch,font=glyph_fonts.get(ch,font),fill=fg,stroke_width=1 if self.bold_cells[r][c] else 0)
        image.save(path)


def main():
    plugin = Path(sys.argv[1]).resolve()  # staged manifest + binary; never overwrite installed app
    out = Path(sys.argv[2]).resolve()
    out.mkdir(parents=True,exist_ok=True)
    original_registry = base.sha256(base.USER_PLUGINS)
    iso = Path(tempfile.mkdtemp(prefix='hseh-visual-'))
    env = {k:os.environ[k] for k in ('PATH','LANG') if k in os.environ}
    env.update(HOME=str(iso),XDG_CONFIG_HOME=str(iso/'.config'),XDG_STATE_HOME=str(iso/'.local/state'),XDG_DATA_HOME=str(iso/'.local/share'),XDG_CACHE_HOME=str(iso/'.cache'),TERM='xterm-256color',HERDR_SESSION='hseh-visual')
    sock = iso/'.config/herdr/sessions/hseh-visual/herdr.sock'
    env['HERDR_SOCKET_PATH'] = str(sock)
    (iso/'.config/herdr').mkdir(parents=True)
    (iso/'.config/herdr/config.toml').write_text('onboarding = false\n[experimental]\nallow_nested = true\n')
    fd=child=server=None
    def herdr(*args):
        result=base.run(env,['herdr',*args])
        if result.returncode: raise AssertionError(result.stderr+result.stdout)
        return result.stdout
    try:
        with (iso/'server.log').open('w') as log:
            server=subprocess.Popen(['herdr','--session','hseh-visual','server'],env=env,stdout=log,stderr=subprocess.STDOUT)
        for _ in range(60):
            if sock.exists(): break
            time.sleep(.1)
        assert sock.exists()
        a=json.loads(herdr('workspace','create','--label','alpha','--cwd',str(iso)))['result']
        b=json.loads(herdr('workspace','create','--label','beta','--cwd',str(iso),'--no-focus'))['result']
        ws_a=a['workspace']['workspace_id']; ws_b=b['workspace']['workspace_id']
        script=iso/'grid.py'
        script.write_text('import sys,shutil,time\ni=0\nwhile True:\n i+=1\n w,h=shutil.get_terminal_size()\n lines=[("BETA_ROW_%02d"%r).ljust(w-1) for r in range(h)]\n lines[-1]=("BETA_LAST_%d"%i).ljust(w-1)\n sys.stdout.write("\\x1b[H\\x1b[2J"+"\\r\\n".join(lines));sys.stdout.flush();time.sleep(.3)\n')
        herdr('pane','run',b['root_pane']['pane_id'],f'python3 -u {script}')
        herdr('plugin','link',str(plugin))
        cfg=Path(herdr('plugin','config-dir','hseh').strip());cfg.mkdir(parents=True,exist_ok=True)
        (cfg/'hseh.toml').write_text('wide_preview_min_columns = 40\n')
        child,fd=pty.fork()
        if child==0:
            os.environ.clear();os.environ.update(env)
            fcntl.ioctl(1,termios.TIOCSWINSZ,struct.pack('HHHH',ROWS,COLS,0,0))
            os.execvp('herdr',['herdr','--session','hseh-visual'])
        fcntl.ioctl(fd,termios.TIOCSWINSZ,struct.pack('HHHH',ROWS,COLS,0,0))
        acc=bytearray();base.acc_drain(fd,1.5,acc)
        herdr('plugin','action','invoke','hseh.spaces')
        def capture(name,delay=.8):
            base.acc_drain(fd,delay,acc)
            screen=StyledScreen();screen.feed(acc.decode('utf8','replace'))
            (out/(name+'.raw')).write_bytes(acc)
            (out/(name+'.txt')).write_text(screen.dump())
            return screen
        def focus():
            data=json.loads(herdr('workspace','list'))
            return base.focused_ids(data)
        initial=capture('spaces',1.5)
        initial.colored_label('Spaces',ACTIVE)
        initial.selected_label('beta')
        # Spaces preview the selected row's directory (issue #3), never the pane: beta's cwd holds grid.py.
        assert 'grid.py' in initial.dump(), initial.dump()
        assert 'BETA_LAST_' not in initial.dump(), 'spaces preview showed pane output'
        assert focus()==[ws_a]
        initial.png(out/'spaces.png')
        os.write(fd,b'\t');agents=capture('agents');agents.colored_label('Agents',ACTIVE)
        os.write(fd,b'\t');spaces=capture('spaces-again');spaces.colored_label('Spaces',ACTIVE)
        os.write(fd,b'be');filtered=capture('query');assert '❯ be' in filtered.dump()
        os.write(fd,b'\x1b[A');selected=capture('selected');selected.selected_label('beta')
        assert 'grid.py' in selected.dump() and 'BETA_LAST_' not in selected.dump()
        settled=capture('updated',1.2)
        assert 'BETA_LAST_' not in settled.dump(), 'directory preview must not poll the pane'
        assert focus()==[ws_a]
        os.write(fd,b'\x1b');closed=capture('escape');assert 'Spaces' not in closed.dump();assert focus()==[ws_a]
        herdr('plugin','action','invoke','hseh.spaces');opened=capture('reopened',1.2)
        # Click alpha, then beta: each selection has a visible full-block background, without focus.
        origin=next(opened.row(r).index('┌hseh') for r in range(ROWS) if '┌hseh' in opened.row(r))
        r=next(r for r in range(ROWS) if opened.row(r).find('alpha')>origin);c=opened.row(r).index('alpha')
        base.sgr_left_click(fd,r,c);clicked=capture('click-alpha');clicked.selected_label('alpha')
        r=next(r for r in range(ROWS) if clicked.row(r).find('beta')>origin);c=clicked.row(r).index('beta')
        base.sgr_left_click(fd,r,c);clicked=capture('click-beta');clicked.selected_label('beta')
        assert focus()==[ws_a]
        os.write(fd,b'\r');capture('enter',1.2);assert focus()==[ws_b]
        assert base.sha256(base.USER_PLUGINS)==original_registry
        (out/'notes.txt').write_text('PASS: rail selection; active tab cycling; search; directory preview without pane polling; mouse; Escape unchanged; Enter beta\nBinary '+base.sha256(plugin/'hseh')+'\n')
        print('PASS visual e2e',out)
    finally:
        if fd is not None: os.close(fd)
        if child: os.waitpid(child,0)
        if sock.exists(): base.run(env,['herdr','server','stop'])
        if server is not None:
            try: server.wait(timeout=8)
            except subprocess.TimeoutExpired: raise AssertionError('isolated server did not stop')
        shutil.rmtree(iso)
        assert base.sha256(base.USER_PLUGINS)==original_registry

if __name__=='__main__': main()
