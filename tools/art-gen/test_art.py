"""Tests of the chance artwork tools (docs/ART.md).

    python3 -m unittest discover -s tools/art-gen

The packing tests need Pillow and are skipped without it.
"""
import contextlib, copy, io, json, os, shutil, subprocess, sys, tempfile, unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory, prompts, generate  # noqa: E402

try:
    import PIL  # noqa: F401
    import pack, placeholders
except ImportError:
    pack = placeholders = None


class Inventory(unittest.TestCase):
    def setUp(self):
        self.reg = inventory.load_registry()
        self.faces = inventory.build(self.reg)

    def test_every_deck_card_has_a_face(self):
        for s in spec.SHEETS:
            ids = {f['id'] for f in self.faces[s.name]}
            for d in s.decks:
                deck = next(x for x in self.reg['decks'] if x['name'] == d)
                self.assertLessEqual(set(deck['cards']), ids, d)

    def test_every_deck_has_a_sheet(self):
        claimed = {d for s in spec.SHEETS for d in s.decks}
        self.assertEqual(claimed, {d['name'] for d in self.reg['decks']})

    def test_counts(self):
        for name, (cells, origs) in inventory.EXPECTED.items():
            self.assertEqual(len(self.faces[name]), cells, name)
            self.assertEqual(len(inventory.originals(self.faces[name])), origs, name)

    def test_italian_courts_are_cut_from_the_spanish(self):
        latin = {f['id']: f for f in self.faces['latin']}
        self.assertEqual(latin['cups-fante']['original'], 'cups-sota')
        self.assertEqual(latin['cups-fante']['index'], 'F')
        self.assertEqual(latin['cups-sota']['index'], 'S')

    def test_red_indices(self):
        fr = {f['id']: f for f in self.faces['french']}
        self.assertTrue(fr['hearts-2']['red'] and fr['diamonds-king']['red'] and fr['joker-red']['red'])
        self.assertFalse(fr['spades-2']['red'] or fr['joker-black']['red'] or fr['back']['index'])

    def test_drift_fails_loudly(self):
        reg = copy.deepcopy(self.reg)
        reg['decks'].append({'name': 'uno', 'cards': ['red-1']})
        with self.assertRaises(inventory.Drift):
            inventory.build(reg)
        reg = copy.deepcopy(self.reg)
        next(d for d in reg['decks'] if d['name'] == 'austrian')['cards'].append('acorns-6')
        with self.assertRaises(inventory.Drift):
            inventory.build(reg)
        reg = copy.deepcopy(self.reg)
        next(d for d in reg['decks'] if d['name'] == 'standard')['cards'].append('stars-ace')
        with self.assertRaises(inventory.Drift):
            inventory.build(reg)

    def test_shared_table_is_current(self):
        with open(inventory.OUT_JSON) as f:
            self.assertEqual(json.load(f), inventory.table(self.reg),
                             'run python3 tools/art-gen/inventory.py --write')


class Prompts(unittest.TestCase):
    def test_every_original_has_a_prompt(self):
        faces = inventory.build(inventory.load_registry())
        for s in spec.SHEETS:
            got = {f for _, f, _ in prompts.jobs(s.name)}
            self.assertEqual(got, set(inventory.originals(faces[s.name])), s.name)
        self.assertEqual(sum(len(prompts.jobs(s.name)) for s in spec.SHEETS), 345)

    def test_style_words(self):
        for s in spec.SHEETS:
            for _, fid, p in prompts.jobs(s.name):
                for hexv in spec.PALETTE.values():
                    self.assertIn(hexv.upper(), p, fid)
                self.assertEqual(p.count(prompts.DONT_START), 1, fid)
                before, after = p.split(prompts.DONT_START)
                # The "do not" list runs to the next full stop.
                rest = before + after.split('.', 1)[1]
                low = rest.lower()
                for w in prompts.FORBIDDEN:
                    self.assertNotIn(w, low, '%s/%s says %r outside the "do not" list' % (s.name, fid, w))
                self.assertIn('Subject: ', p)

    def test_reference_board(self):
        board = prompts.jobs('reference-board')
        self.assertEqual([(a, b) for a, b, _ in board], prompts.REFERENCE_BOARD)
        for sheet, fid, p in board:
            self.assertEqual(p, prompts.prompt(sheet, fid))


class Generate(unittest.TestCase):
    def test_prompt_is_quoted_for_the_remote_shell(self):
        # The script runs under a real /bin/sh with a stand-in for
        # nano-banana that records its arguments, in a scratch home.
        with tempfile.TemporaryDirectory() as home:
            fake = os.path.join(home, 'fake-banana')
            with open(fake, 'w') as f:
                f.write('#!/bin/sh\nfor a in "$@"; do printf "%s\\0" "$a"; done > "$HOME/args"\n')
            os.chmod(fake, 0o755)
            args = mock.Mock(binary='~/fake-banana', size='1K', aspect=None, model='flash', remote_path='/usr/bin:/bin')
            nasty = 'a "quote" and \'another\' $(touch x) `id`; echo hi > y \n next line $HOME'
            script = generate.remote_script(args, 'tarot', 'major-16', nasty, ['ref.png'])
            subprocess.run(['/bin/sh', '-s'], input=script, text=True, env={'HOME': home, 'PATH': '/usr/bin:/bin'},
                           capture_output=True)
            with open(os.path.join(home, 'args')) as f:
                words = f.read().split('\0')[:-1]
            self.assertEqual(sorted(os.listdir(home)), ['args', 'churr-art', 'fake-banana'])
            self.assertEqual(sorted(os.listdir(os.path.join(home, 'churr-art'))), ['out', 'refs'])
        self.assertEqual(words[0], nasty)
        self.assertEqual(words[1:], ['-o', 'tarot--major-16', '-d', 'out', '-s', '1K', '-a', '2:3', '-m', 'flash',
                                     '-r', 'refs/ref.png'])

    def test_dry_run_sends_nothing(self):
        with mock.patch('subprocess.run', side_effect=AssertionError('no request on a dry run')), \
                contextlib.redirect_stdout(io.StringIO()) as out, tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(generate.main(['--set', 'reference-board', '--dry-run', '--out', tmp]), 0)
        self.assertIn('6 to generate', out.getvalue())

    def test_resumes(self):
        with tempfile.TemporaryDirectory() as tmp, contextlib.redirect_stdout(io.StringIO()) as out:
            os.makedirs(os.path.join(tmp, 'dice-cube'))
            open(os.path.join(tmp, 'dice-cube', 'd6-1.png'), 'wb').close()
            generate.main(['--set', 'dice-cube', '--dry-run', '--out', tmp])
        self.assertIn('5 to generate, 1 already there', out.getvalue())


@unittest.skipIf(pack is None, 'needs Pillow')
class Pack(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.mkdtemp(prefix='churr-art-')
        with contextlib.redirect_stdout(io.StringIO()):
            placeholders.main(['--out', os.path.join(cls.tmp, 'ph')])

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp)

    def test_every_sheet_indexes_every_face(self):
        faces = inventory.build(inventory.load_registry())
        web, app = os.path.join(self.tmp, 'web'), os.path.join(self.tmp, 'app')
        with contextlib.redirect_stdout(io.StringIO()):
            pack.main(['--originals', os.path.join(self.tmp, 'ph'), '--web', web, '--app', app])
        from PIL import Image
        for s in spec.SHEETS:
            with open(os.path.join(web, s.name + '.json')) as f:
                idx = json.load(f)
            self.assertEqual(list(idx['faces']), [x['id'] for x in faces[s.name]], s.name)
            self.assertEqual(idx['cell'], list(s.cell))
            cells = {tuple(v) for v in idx['faces'].values()}
            self.assertEqual(len(cells), len(idx['faces']), 'one cell per face')
            for col, row in cells:
                self.assertLess(col, idx['columns'])
                self.assertLess(row, idx['rows'])
            with Image.open(os.path.join(web, s.name + '.webp')) as im:
                self.assertEqual(list(im.size), idx['size'])
                self.assertEqual(im.mode, 'RGBA')
                cw, ch = s.cell
                if s.cut:  # the cut corner is transparent, the opposite corner is not
                    self.assertEqual(im.getpixel((cw - 2, 1))[3], 0)
                    self.assertEqual(im.getpixel((1, 1))[3], 255)
            with open(os.path.join(app, s.name + '.webp'), 'rb') as a, \
                    open(os.path.join(web, s.name + '.webp'), 'rb') as b:
                self.assertEqual(a.read(), b.read())
            if s.kind == 'poly':
                self.assertEqual(set(idx['text']), set(idx['faces']))
                for box in idx['text'].values():
                    self.assertTrue(0 <= box['x'] and box['x'] + box['w'] <= cw and box['w'] > 10)
        with open(os.path.join(web, 'sheets.js')) as f:
            self.assertIn('"french"', f.read())

    def test_deterministic(self):
        ph = os.path.join(self.tmp, 'ph')
        for name in ['german', 'dice-cube', 'dice-poly']:
            a = pack.pack_sheet(name, ph)
            b = pack.pack_sheet(name, ph)
            self.assertEqual(a[0], b[0], name)
            self.assertEqual(a[1], b[1], name)

    def test_missing_original_fails(self):
        empty = os.path.join(self.tmp, 'empty')
        os.makedirs(empty, exist_ok=True)
        with self.assertRaises(SystemExit):
            pack.pack_sheet('dice-cube', empty)


if __name__ == '__main__':
    unittest.main()
