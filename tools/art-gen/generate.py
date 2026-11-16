#!/usr/bin/env python3
"""Generates the originals of the chance artwork with the `nano-banana`
command on another machine, over ssh, and copies each picture back
(docs/ART.md).

    python3 tools/art-gen/generate.py --set reference-board --dry-run
    python3 tools/art-gen/generate.py --set reference-board
    python3 tools/art-gen/generate.py --set french --reference art-originals/french/spades-ace.png
    python3 tools/art-gen/generate.py --set tarot --only major-16

The Gemini key lives on that machine, in nano-banana's own setup; nothing
here reads, sends or prints one. Only the prompt and the reference
pictures leave this machine. Each original lands in
<out>/<sheet>/<face>.<ext>, with the prompt beside it as <face>.txt; a face
whose picture is already there is skipped, so a run that stops can simply
be started again.
"""
import argparse, glob, os, re, shlex, subprocess, sys
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import spec, prompts  # noqa: E402

# The remote working folder, relative to the remote home. Fixed on purpose:
# it is the only place this script writes on the remote machine.
REMOTE_DIR = 'churr-art'
# nano-banana's own price list (its --help), per image on the flash model;
# pro costs twice as much.
PRICE = {'512': 0.045, '1K': 0.067, '2K': 0.101, '4K': 0.151}
ASPECTS = {'1:1', '16:9', '9:16', '4:3', '3:4', '3:2', '2:3', '4:5', '5:4', '21:9'}
IMAGE_EXT = ('.png', '.jpg', '.jpeg', '.webp')


def existing(out, sheet, face):
    for ext in IMAGE_EXT:
        p = os.path.join(out, sheet, face + ext)
        if os.path.exists(p):
            return p
    return None


def remote_name(sheet, face):
    return '%s--%s' % (sheet, face)


def safe_base(path):
    return re.sub(r'[^A-Za-z0-9._-]', '_', os.path.basename(path))


def run(cmd, stdin=None):
    return subprocess.run(cmd, input=stdin, capture_output=True, text=True)


def home(path):
    """A remote path, quoted, with a leading ~/ read as the remote home."""
    if path.startswith('~/'):
        return '"$HOME"/' + shlex.quote(path[2:])
    return shlex.quote(path)


def remote_script(args, sheet, face, prompt, refs):
    """A POSIX shell script for the remote machine, fed on stdin to
    /bin/sh (the remote login shell may not be POSIX). Every value is
    quoted with shlex.quote, so nothing in a prompt reaches the shell."""
    q = shlex.quote
    cmd = [args.binary, prompt, '-o', remote_name(sheet, face), '-d', 'out', '-s', args.size,
           '-a', prompts.aspect(sheet, face) if args.aspect is None else args.aspect, '-m', args.model]
    for r in refs:
        cmd += ['-r', 'refs/' + r]
    return '\n'.join([
        'set -e',
        'cd "$HOME"',
        'mkdir -p %s/out %s/refs' % (REMOTE_DIR, REMOTE_DIR),
        'cd %s' % REMOTE_DIR,
        'PATH=%s:"$PATH"; export PATH' % q(args.remote_path),
        ' '.join([home(cmd[0])] + [q(c) for c in cmd[1:]]),
        # The file the command wrote, whatever its extension.
        'for f in out/%s.*; do [ -f "$f" ] && printf "RESULT %%s\\n" "$f"; done' % remote_name(sheet, face),
        '',
    ])


def upload_refs(args):
    names = []
    for r in args.reference:
        if not os.path.isfile(r):
            sys.exit('no reference picture at %s' % r)
        base = safe_base(r)
        mk = run(['ssh', args.host, '/bin/sh', '-s'], 'mkdir -p "$HOME"/%s/refs\n' % REMOTE_DIR)
        if mk.returncode:
            sys.exit('ssh %s failed: %s' % (args.host, mk.stderr.strip()))
        up = run(['scp', '-q', r, '%s:%s/refs/%s' % (args.host, REMOTE_DIR, base)])
        if up.returncode:
            sys.exit('copying %s failed: %s' % (r, up.stderr.strip()))
        names.append(base)
    return names


def generate_one(args, refs, sheet, face, prompt):
    if refs:
        prompt = prompt + ' ' + prompts.REFERENCE_NOTE
    gen = run(['ssh', args.host, '/bin/sh', '-s'], remote_script(args, sheet, face, prompt, refs))
    results = [l[7:].strip() for l in gen.stdout.splitlines() if l.startswith('RESULT ')]
    if gen.returncode or not results:
        last = ((gen.stderr or gen.stdout).strip().splitlines() or ['no output'])[-1]
        return '%s/%s: failed (%s)' % (sheet, face, last)
    remote = results[0]
    ext = os.path.splitext(remote)[1].lower() or '.png'
    folder = os.path.join(args.out, sheet)
    os.makedirs(folder, exist_ok=True)
    local = os.path.join(folder, face + ext)
    got = run(['scp', '-q', '%s:%s/%s' % (args.host, REMOTE_DIR, remote), local + '.part'])
    if got.returncode:
        return '%s/%s: copying back failed (%s)' % (sheet, face, got.stderr.strip())
    os.replace(local + '.part', local)
    with open(os.path.join(folder, face + '.txt'), 'w') as f:
        f.write(prompt + '\n')
    return '%s/%s: %s' % (sheet, face, os.path.relpath(local))


def main(argv=None):
    sets = ['reference-board'] + [s.name for s in spec.SHEETS]
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--set', required=True, choices=sets)
    ap.add_argument('--only', action='append', default=[], metavar='FACE', help='only this original (repeatable)')
    ap.add_argument('--reference', action='append', default=[], metavar='IMAGE',
                    help='a picture given to the model for consistency (repeatable)')
    ap.add_argument('--out', default=spec.ORIGINALS, help='where originals go (default: art-originals/, ignored by git)')
    ap.add_argument('--host', default='pole.lan', help='the machine with nano-banana (default pole.lan)')
    ap.add_argument('--binary', default='~/.bun/bin/nano-banana', help='nano-banana on that machine')
    ap.add_argument('--remote-path', default='/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin',
                    help='PATH there, where bun is found')
    ap.add_argument('--model', default='flash', help='flash (default), pro, or a Gemini model id')
    ap.add_argument('--size', default='1K', choices=sorted(PRICE))
    ap.add_argument('--aspect', default=None, choices=sorted(ASPECTS), help="default: the sheet's own")
    ap.add_argument('--jobs', type=int, default=1, help='requests at once (default 1)')
    ap.add_argument('--dry-run', action='store_true', help='print the prompts and the count; send nothing')
    args = ap.parse_args(argv)

    todo = prompts.jobs(args.set)
    if args.only:
        known = {f for _, f, _ in todo}
        unknown = [o for o in args.only if o not in known]
        if unknown:
            sys.exit('not in %s: %s' % (args.set, ', '.join(unknown)))
        todo = [j for j in todo if j[1] in args.only]
    skipped = [j for j in todo if existing(args.out, j[0], j[1])]
    todo = [j for j in todo if not existing(args.out, j[0], j[1])]
    each = PRICE[args.size] * (2 if args.model in ('pro', 'nb-pro') else 1)
    if args.model not in ('flash', 'nb2', 'pro', 'nb-pro'):
        each = None

    if args.dry_run:
        for sheet, face, p in todo:
            aspect = args.aspect or prompts.aspect(sheet, face)
            print('== %s/%s  (-s %s -a %s -m %s%s)\n%s\n' % (
                sheet, face, args.size, aspect, args.model,
                ''.join(' -r ' + safe_base(r) for r in args.reference), p))
    cost = ('about $%.2f' % (each * len(todo))) if each is not None else 'unknown for this model'
    print('%d to generate, %d already there; %s at %s on %s' % (len(todo), len(skipped), cost, args.size, args.model))
    if args.dry_run or not todo:
        return 0

    refs = upload_refs(args)
    failed = 0
    with ThreadPoolExecutor(max_workers=max(1, args.jobs)) as pool:
        for line in pool.map(lambda j: generate_one(args, refs, *j), todo):
            print(line, flush=True)
            failed += 'failed' in line
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
