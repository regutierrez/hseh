#!/usr/bin/env python3
"""Sesh-style Spaces viewport (issue #3): source badges, absolute paths, directory listing preview,
path column hiding when narrow, recovery-needed row, and Enter on a template creating its space.
Isolated HOME/XDG/Herdr session; reuses the v11 styled terminal driver. No model calls."""
import importlib.util, json, os, pty, fcntl, shutil, struct, subprocess, sys, tempfile, termios, time
from pathlib import Path

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('visual', HERE.parent / 'v11/run_visuals.py')
v = importlib.util.module_from_spec(spec); spec.loader.exec_module(v)
base = v.base
WIDE, NARROW = 200, 90  # herdr keeps a 26-col sidebar; an 85% popup of the rest split in half must leave the list >= 45 cells for the path column
ROWS = 40


def main():
    plugin = Path(sys.argv[1]).resolve()  # staged manifest + binary; never the installed app
    out = Path(sys.argv[2]).resolve(); out.mkdir(parents=True, exist_ok=True)
    registry_before = base.sha256(base.USER_PLUGINS)
    iso = Path(tempfile.mkdtemp(prefix='hseh-viewport-'))
    env = {k: os.environ[k] for k in ('PATH', 'LANG') if k in os.environ}
    env.update(HOME=str(iso), XDG_CONFIG_HOME=str(iso / '.config'), XDG_STATE_HOME=str(iso / '.local/state'),
               XDG_DATA_HOME=str(iso / '.local/share'), XDG_CACHE_HOME=str(iso / '.cache'), TERM='xterm-256color',
               HERDR_SESSION='hseh-viewport')
    sock = iso / '.config/herdr/sessions/hseh-viewport/herdr.sock'
    env['HERDR_SOCKET_PATH'] = str(sock)
    (iso / '.config/herdr').mkdir(parents=True)
    (iso / '.config/herdr/config.toml').write_text('onboarding = false\n[experimental]\nallow_nested = true\n')
    # Fixture directories: a git repo with a subdirectory (checkout-root path), a template dir, a recovery dir.
    repo, sub, tmpl, rec = iso / 'repo', iso / 'repo/sub', iso / 'tmpl', iso / 'rec'
    for d in (sub, tmpl, rec):
        d.mkdir(parents=True)
    (repo / 'README_MARKER.md').write_text('marker\n'); (tmpl / 'GAMMA_MARKER.txt').write_text('marker\n'); (rec / 'REC_MARKER.txt').write_text('marker\n')
    git_env = dict(env, GIT_AUTHOR_NAME='t', GIT_AUTHOR_EMAIL='t@x', GIT_COMMITTER_NAME='t', GIT_COMMITTER_EMAIL='t@x')
    for args in (['init', '-q', '-b', 'main'], ['add', '.'], ['commit', '-q', '-m', 'init']):
        subprocess.run(['git', '-C', str(repo), *args], env=git_env, check=True, capture_output=True)
    fd = child = server = None
    slog = (iso / 'server.log').open('a')

    def herdr(*args):
        r = base.run(env, ['herdr', *args])
        if r.returncode: raise AssertionError(r.stderr + r.stdout)
        return r.stdout

    def start_server():
        proc = subprocess.Popen(['herdr', '--session', 'hseh-viewport', 'server'], env=env, stdout=slog, stderr=subprocess.STDOUT)
        for _ in range(60):
            if sock.exists(): return proc
            time.sleep(.1)
        raise AssertionError('isolated socket missing')

    def workspaces():
        return json.loads(herdr('workspace', 'list'))['result']['workspaces']

    def focused():
        return [w['workspace_id'] for w in workspaces() if w.get('focused')]

    try:
        server = start_server()
        a = json.loads(herdr('workspace', 'create', '--label', 'alpha', '--cwd', str(sub)))['result']
        b = json.loads(herdr('workspace', 'create', '--label', 'beta', '--cwd', str(iso), '--no-focus'))['result']
        ws_a, ws_b = a['workspace']['workspace_id'], b['workspace']['workspace_id']
        herdr('plugin', 'link', str(plugin))
        cfg = Path(herdr('plugin', 'config-dir', 'hseh').strip()); (cfg / 'spaces').mkdir(parents=True, exist_ok=True)
        (cfg / 'hseh.toml').write_text('wide_preview_min_columns = 40\n')
        (cfg / 'spaces/gamma.toml').write_text(f'id = "def-gamma"\nname = "gamma"\ndescription = "template fixture"\nworking_dir = "{tmpl}"\n[[tabs]]\nname = "main"\n')
        (cfg / 'spaces/rec.toml').write_text(f'id = "def-rec"\nname = "rec"\nworking_dir = "{rec}"\n[[tabs]]\nname = "main"\n')
        # Open the recovery fixture through the CLI, then cold-restart so its association becomes unresolved.
        cli_env = {k: val for k, val in env.items() if k not in ('HERDR_PLUGIN_CONFIG_DIR', 'HERDR_PLUGIN_STATE_DIR')}
        opened = base.run(cli_env, [str(plugin / 'hseh'), 'open', 'def-rec'])
        assert opened.returncode == 0, opened.stderr + opened.stdout
        herdr('server', 'stop')
        for _ in range(100):
            if not sock.exists() and server.poll() is not None: break
            time.sleep(.1)
        server = start_server()
        herdr('workspace', 'focus', ws_a)
        listing = base.run(cli_env, [str(plugin / 'hseh'), 'list', '--json'])
        (out / 'list.json').write_text(listing.stdout)
        doc = json.loads(listing.stdout)
        by_label = {i['label']: i for i in doc['items']}
        assert by_label['alpha']['source'] == 'herdr' and by_label['alpha']['path'] == str(repo), by_label['alpha']
        assert by_label['gamma']['source'] == 'template' and by_label['gamma']['path'] == str(tmpl), by_label['gamma']
        assert by_label['rec']['recovery'] == ['hseh recover def-rec --workspace <live-workspace-id>', 'hseh recover def-rec --create'], by_label['rec']
        assert all(len(i['rows']) == 1 for i in doc['items']), doc['items']

        child, fd = pty.fork()
        if child == 0:
            os.environ.clear(); os.environ.update(env)
            fcntl.ioctl(1, termios.TIOCSWINSZ, struct.pack('HHHH', ROWS, WIDE, 0, 0))
            os.execvp('herdr', ['herdr', '--session', 'hseh-viewport'])
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', ROWS, WIDE, 0, 0))
        acc = bytearray(); base.acc_drain(fd, 1.5, acc)
        cols = [WIDE]

        def capture(name, delay=.9):
            base.acc_drain(fd, delay, acc)
            v.COLS = cols[0]
            screen = v.StyledScreen(); screen.feed(acc.decode('utf8', 'replace'))
            (out / (name + '.raw')).write_bytes(acc); (out / (name + '.txt')).write_text(screen.dump())
            return screen

        def resize(width):
            cols[0] = width
            fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', ROWS, width, 0, 0))

        def rows_with(screen, text):
            # Popup rows only: every Spaces row carries a source badge, unlike herdr's own sidebar.
            return [screen.row(r) for r in range(ROWS) if text in screen.row(r) and (' herdr ' in screen.row(r) or ' template ' in screen.row(r))]

        def assert_selected(screen, label):
            # The selected item is marked by the accent rail at the list edge and a bold name.
            for r in range(ROWS):
                row = screen.row(r); col = row.find(label)
                if col >= 0 and '┃' in row[:col] and screen.bold_cells[r][col]:
                    return r
            raise AssertionError(f'{label!r} not selected\n{screen.dump()}')

        def assert_active_tab(screen, label):
            # Accent background: 256-colour or truecolour depending on the terminal profile herdr reports.
            for color in (v.ACTIVE, (137, 180, 250)):
                try: return screen.colored_label(label, color)
                except AssertionError: pass
            raise AssertionError(f'{label!r} tab not active\n{screen.dump()}')

        herdr('plugin', 'action', 'invoke', 'hseh.spaces')
        wide = capture('spaces-wide', 1.5); assert_active_tab(wide, 'Spaces')
        dump = wide.dump()
        assert dump.count(' herdr ') >= 3 and dump.count(' template ') >= 2, dump
        alpha_row = rows_with(wide, 'alpha')[0]
        assert str(repo) in alpha_row and str(sub) not in alpha_row, 'alpha path is not the checkout root: ' + alpha_row
        assert ' main' in alpha_row, 'alpha row lacks git branch: ' + alpha_row
        rec_rows = rows_with(wide, '(recovery needed)')
        assert rec_rows and 'template' in rec_rows[0] and str(rec) in rec_rows[0], rec_rows
        assert 'template fixture' not in dump, 'description rendered in a row'
        wide.png(out / 'spaces-wide.png')

        # A query that drops the selected row clears the selection (SPEC); Up picks the top match, as a user would.
        os.write(fd, b'gamma\x1b[A'); gamma = capture('template-preview'); assert_selected(gamma, 'gamma')
        assert 'GAMMA_MARKER.txt' in gamma.dump(), gamma.dump()
        assert focused() == [ws_a]
        for _ in range(5): os.write(fd, b'\x7f')
        os.write(fd, b'recovery\x1b[A'); recov = capture('recovery-preview')
        assert 'hseh recover def-rec --create' in recov.dump() and 'REC_MARKER.txt' in recov.dump(), recov.dump()
        for _ in range(8): os.write(fd, b'\x7f')
        os.write(fd, b'alpha\x1b[A'); alpha = capture('space-preview'); assert_selected(alpha, 'alpha')
        assert 'README_MARKER.md' in alpha.dump() and 'sub' in alpha.dump(), alpha.dump()
        assert focused() == [ws_a]

        resize(NARROW); narrow = capture('spaces-narrow', 1.5)
        assert rows_with(narrow, 'alpha') and str(repo) not in narrow.dump(), 'path column still shown when narrow\n' + narrow.dump()
        assert 'README_MARKER.md' in narrow.dump(), 'preview lost when narrow\n' + narrow.dump()
        resize(WIDE); back = capture('spaces-wide-again', 1.5)
        assert str(repo) in rows_with(back, 'alpha')[0], back.dump()
        assert focused() == [ws_a]

        for _ in range(5): os.write(fd, b'\x7f')
        os.write(fd, b'gamma\x1b[A'); before = capture('before-enter'); assert_selected(before, 'gamma'); os.write(fd, b'\r'); capture('enter', 1.5)
        created = [w for w in workspaces() if w.get('label') == 'gamma']
        assert len(created) == 1 and focused() == [created[0]['workspace_id']], workspaces()
        assert base.sha256(base.USER_PLUGINS) == registry_before
        (out / 'notes.txt').write_text('PASS: badges, checkout-root path, one-line rows, template/recovery/space directory previews, '
                                       'narrow hides path, Enter on template creates gamma\nBinary ' + base.sha256(plugin / 'hseh') + '\n')
        print('PASS spaces viewport', out)
    finally:
        if fd is not None: os.close(fd)
        if child: os.waitpid(child, 0)
        if sock.exists(): base.run(env, ['herdr', 'server', 'stop'])
        if server is not None:
            try: server.wait(timeout=8)
            except subprocess.TimeoutExpired: raise AssertionError('isolated server did not stop')
        slog.close(); shutil.rmtree(iso)
        assert base.sha256(base.USER_PLUGINS) == registry_before


if __name__ == '__main__': main()
