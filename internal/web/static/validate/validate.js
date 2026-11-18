// The "Validate result" page (docs/DESIGN-FAIR.md section 12), on every
// instance and hub, for anyone with no account: the fields of a draw, or its
// one JSON document (docs/FAIR.md section 11), checked in the browser by the
// same code the chat checks with (shared/fair/validate.js). It reads a draw
// from the address's fragment, which no server is ever sent, and asks
// nothing of any server unless a server is named, whose published key
// history it then fetches, or reads from this browser's pin of it.

import { validate, readDocument, makeDocument, fragmentOf, fromFragment, fetchHistory, SPEC, LIBRARY } from '/shared/fair/validate.js';

const $ = (id) => document.getElementById(id);
const said = (t) => { $('said').textContent = t; };

// The fields as a document, or null with why.
function fromFields() {
  let objects = [];
  const raw = $('objects').value.trim();
  if (raw) {
    try { objects = JSON.parse(raw); } catch { said('The primitives are not JSON.'); return null; }
    if (!Array.isArray(objects)) { said('The primitives are a list of objects.'); return null; }
  }
  return makeDocument({ alpha: $('alpha').value.trim(), beta: $('beta').value.trim(), proof: $('proof').value.trim(), pk: $('pk').value.trim(), owner: $('server').value.trim(), objects });
}

function fill(doc) {
  $('alpha').value = doc.alpha || '';
  $('beta').value = doc.beta || '';
  $('proof').value = doc.proof || '';
  $('pk').value = doc.pk || '';
  $('server').value = doc.owner || '';
  $('objects').value = JSON.stringify(doc.objects || [], null, 1);
  $('doc').value = JSON.stringify(doc, null, 1);
}

// The history of the server named: its /.well-known/churr, or, when that
// cannot be reached, the pin this browser keeps of it (the chat's).
async function keysOf(name) {
  if (!name) return null;
  const fetched = name === location.host
    ? await fetchHistory(name, (_, o) => fetch('/.well-known/churr', o))
    : await fetchHistory(name);
  if (fetched.history) return fetched;
  try {
    const pin = JSON.parse(localStorage.getItem('churr.fair.pin.' + name) || 'null');
    if (Array.isArray(pin) && pin.length) return { name, history: pin };
  } catch { /* no pin */ }
  return fetched;
}

const WORD = { proof: 'The proof', beta: 'The output', outcome: 'An outcome', alpha: 'The input', key: 'The key' };

function draw(verdicts) {
  const list = $('verdicts');
  list.replaceChildren();
  for (const v of verdicts) {
    const li = document.createElement('li');
    const state = v.ok === true ? 'pass' : v.ok === false ? 'fail' : 'none';
    li.className = 'verdict ' + state;
    const mark = document.createElement('span');
    mark.className = 'mark';
    mark.textContent = state === 'pass' ? '✓ Pass' : state === 'fail' ? '✗ Fail' : '– Not said';
    const head = document.createElement('strong');
    head.textContent = v.id === 'outcome' && v.op ? `Outcome of object ${v.i}, primitive ${v.j + 1} (${v.op})` : WORD[v.id];
    const why = document.createElement('span');
    why.className = 'why';
    why.textContent = v.why;
    li.append(mark, head, why);
    list.append(li);
  }
}

async function check() {
  const doc = fromFields();
  if (!doc) return;
  $('doc').value = JSON.stringify(doc, null, 1);
  said('Checking…');
  const keys = await keysOf(doc.owner);
  draw(await validate(doc, keys));
  said('Checked here, in this page.');
}

function current() {
  const doc = fromFields();
  return doc && JSON.stringify(doc, null, 1);
}

$('check').addEventListener('click', check);
$('read-doc').addEventListener('click', () => {
  const r = readDocument($('doc').value);
  if (r.error) { said(r.error); return; }
  fill(r.doc);
  check();
});
$('copy').addEventListener('click', async () => {
  const text = current();
  if (!text) return;
  try { await navigator.clipboard.writeText(text); said('The document is copied.'); } catch { $('doc').value = text; said('Copying was not allowed: the document is in the box above.'); }
});
$('download').addEventListener('click', () => {
  const text = current();
  if (!text) return;
  const a = document.createElement('a');
  a.href = URL.createObjectURL(new Blob([text + '\n'], { type: 'application/json' }));
  a.download = 'churr-draw.json';
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
});
$('link').addEventListener('click', async () => {
  const doc = fromFields();
  if (!doc) return;
  const url = location.origin + location.pathname + fragmentOf(doc);
  try { await navigator.clipboard.writeText(url); said('A link with the draw after the # is copied.'); } catch { said(url); }
});

// The pointers the document carries too.
$('spec').href = SPEC;
$('library').href = LIBRARY;

const given = fromFragment(location.hash);
if (given) { fill(given); check(); }
