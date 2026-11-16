"""Tests of the chance artwork tools (docs/ART.md).

    python3 -m unittest discover -s tools/art-gen

The packing tests need Pillow and are skipped without it.
"""
import contextlib, copy, io, json, os, re, shutil, subprocess, sys, tempfile, unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, inventory, prompts, generate  # noqa: E402

try:
    import numpy, scipy, vtracer, resvg_py, fontTools  # noqa: F401
    import PIL  # noqa: F401
    import pack, placeholders, trace
except ImportError:
    pack = placeholders = trace = None


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
        self.assertEqual((fr['hearts-2']['mark'], fr['joker-red']['mark'], fr['back']['mark']), ('hearts', None, None))
        self.assertEqual((fr['hearts-7']['build'], fr['hearts-7']['original'], fr['hearts-7']['pips']), ('pips', 'mark-hearts', 7))
        self.assertEqual(fr['hearts-queen']['build'], 'double')
        self.assertEqual({f['build'] for f in self.faces['dice-cube']}, {'rule'})
        # Only the French deck has the red and black convention.
        for name in ['tarot', 'latin', 'german', 'swiss']:
            self.assertFalse(any(f['red'] or f['mark'] for f in self.faces[name]), name)

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
        self.assertEqual(sum(len(prompts.jobs(s.name)) for s in spec.SHEETS), 223)

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
            os.makedirs(os.path.join(tmp, 'german'))
            open(os.path.join(tmp, 'german', 'acorns-ace.png'), 'wb').close()
            generate.main(['--set', 'german', '--dry-run', '--out', tmp])
        self.assertIn('19 to generate, 1 already there', out.getvalue())


ALLOWED = {h.upper() for h in list(spec.PALETTE.values()) + [spec.SUIT_RED, spec.SUIT_BLACK]}


def hexes(svg):
    return {h.upper() for h in re.findall(r'#[0-9A-Fa-f]{6}\b', svg)}


@unittest.skipIf(trace is None, 'needs numpy, scipy, vtracer, resvg-py, fontTools, Pillow')
class Trace(unittest.TestCase):
    def fixture(self, path):
        from PIL import Image, ImageDraw
        im = Image.new('RGB', (96, 128), (243, 241, 254))
        d = ImageDraw.Draw(im)
        d.rectangle((20, 20, 70, 60), fill=(250, 90, 200), outline=(14, 16, 40), width=4)
        d.polygon([(48, 70), (80, 110), (16, 110)], fill=(210, 30, 45))
        d.ellipse((60, 75, 90, 105), fill=(230, 200, 180))  # an off-palette tint
        im.save(path, quality=90)

    def test_deterministic_and_in_palette(self):
        with tempfile.TemporaryDirectory() as tmp:
            p = os.path.join(tmp, 'x.jpeg')
            self.fixture(p)
            a, b = trace.trace(p), trace.trace(p)
            self.assertEqual(a, b)
            self.assertTrue(a['paths'])
            self.assertLessEqual({c for c, _ in a['paths']}, ALLOWED)
            self.assertNotIn(spec.PAPER.upper(), {c for c, _ in a['paths'][:1]}, 'the paper ground is dropped')
            self.assertEqual((a['w'], a['h']), (192, 256))
            x0, y0, x1, y1 = a['box']
            self.assertTrue(30 <= x0 < x1 <= 192 and 30 <= y0 < y1 <= 230, a['box'])


@unittest.skipIf(pack is None, 'needs numpy, scipy, vtracer, resvg-py, fontTools, Pillow')
class Pack(unittest.TestCase):
    SETS = ['french', 'dice-cube', 'dice-poly']

    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.mkdtemp(prefix='churr-art-')
        with contextlib.redirect_stdout(io.StringIO()):
            placeholders.main(['--out', os.path.join(cls.tmp, 'ph'), '--set', 'french', '--scale', '0.25'])
            pack.main(['--originals', os.path.join(cls.tmp, 'ph'), '--web', os.path.join(cls.tmp, 'web'),
                       '--app', os.path.join(cls.tmp, 'app')] + sum((['--set', s] for s in cls.SETS), []))

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp)

    def read(self, *p):
        with open(os.path.join(self.tmp, *p)) as f:
            return f.read()

    def test_sprite_has_every_face_and_resolves(self):
        faces = inventory.build(inventory.load_registry())
        for name in self.SETS:
            svg = self.read('web', name + '.svg')
            idx = json.loads(self.read('web', name + '.json'))
            want = [f['id'] for f in faces[name]]
            self.assertEqual(idx['faces'], want)
            symbols = re.findall(r'<symbol id="([^"]+)"', svg)
            self.assertEqual(symbols, want, name)
            ids = set(re.findall(r' id="([^"]+)"', svg))
            for ref in set(re.findall(r'href="#([^"]+)"', svg)) | set(re.findall(r'url\(#([^)]+)\)', svg)):
                self.assertIn(ref, ids, '%s: <use> of #%s, which is not there' % (name, ref))
            self.assertLessEqual(hexes(svg), ALLOWED, name)
            self.assertNotIn('<text', svg)
            self.assertNotIn('font-family', svg)

    def test_app_atlas(self):
        from PIL import Image
        for name in self.SETS:
            idx = json.loads(self.read('app', name + '.json'))
            web = json.loads(self.read('web', name + '.json'))
            self.assertEqual(list(idx['faces']), web['faces'])
            with Image.open(os.path.join(self.tmp, 'app', name + '.webp')) as im:
                self.assertEqual(im.size, (round(idx['size'][0] * pack.ATLAS_SCALE), round(idx['size'][1] * pack.ATLAS_SCALE)))
                cw, ch = idx['cell']
                k = pack.ATLAS_SCALE
                if spec.BY_NAME[name].cut:  # the cut corner is transparent, the opposite corner is not
                    self.assertEqual(im.getpixel((round(cw * k) - 2, 1))[3], 0)
                    self.assertEqual(im.getpixel((2, 2))[3], 255)

    def test_rendered_cards(self):
        from PIL import Image
        svg = self.read('web', 'french.svg')
        img = pack.render_face(svg, 'hearts-7', 176, 246).convert('RGB')
        colours = {c for _, c in img.getcolors(1 << 16)}
        red = tuple(int(spec.SUIT_RED[i:i + 2], 16) for i in (1, 3, 5))
        self.assertIn(red, colours, 'the index of a heart is suit red')
        back = pack.render_face(svg, 'back', 176, 246).convert('RGB')
        lav = tuple(int(spec.LAVENDER[i:i + 2], 16) for i in (1, 3, 5))
        self.assertEqual(back.getpixel((88, 30 + 7)) in (lav, tuple(int(spec.INK[i:i + 2], 16) for i in (1, 3, 5))), True)

    def test_deterministic(self):
        ph = os.path.join(self.tmp, 'ph')
        cache = os.path.join(self.tmp, 'cache2')
        for name in self.SETS:
            a = pack.build_sheet(name, ph, cache=cache)
            b = pack.build_sheet(name, ph, cache=os.path.join(self.tmp, 'cache3'))
            self.assertEqual(a[0], b[0], name)
            self.assertEqual(a[0], self.read('web', name + '.svg'), name)

    def test_missing_original_fails(self):
        with self.assertRaises(SystemExit):
            pack.build_sheet('french', os.path.join(self.tmp, 'empty'), cache=os.path.join(self.tmp, 'c4'))


if __name__ == '__main__':
    unittest.main()
