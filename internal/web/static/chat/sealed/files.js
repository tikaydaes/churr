// Files in a sealed chat (docs/SEALED.md section 9): encrypted in the
// browser in chunks before upload, with progress, and decrypted chunk by
// chunk after download. A file is handed over only once its last chunk has
// checked out. For an image the browser also makes the thumbnail itself
// (at most 720 pixels, re-encoded, so location and camera details are
// gone) and encrypts it under a second key.

import { upload } from '/shared/api.js';
import { encryptStream, decryptStream, progressStream, collect, randomBytes, blobSize, fileEntry, fromB64 } from '/shared/sealed/index.js';

// sealFile encrypts and uploads one file to a sealed chat. onProgress gets
// a fraction and a word. It returns the entry for the sealed message.
export async function sealFile(conv, file, onProgress) {
  const key = randomBytes(32);
  const total = file.size;
  const blob = await collect(file.stream().pipeThrough(progressStream((n) => onProgress(total ? (n / total) * 0.4 : 0.4, 'Encrypting'))).pipeThrough(encryptStream(key)));
  if (blob.size !== blobSize(file.size)) throw new Error('The file changed while it was being encrypted.');
  const id = await send(conv, blob, (f) => onProgress(0.4 + f * 0.5, 'Uploading'));
  const entry = { id, key, name: file.name || 'file', mediaType: file.type || 'application/octet-stream', kind: kindOf(file.type), size: file.size };
  if (entry.kind === 'image') {
    try {
      const t = await thumbnail(file);
      if (t) {
        entry.width = t.width; entry.height = t.height;
        const tkey = randomBytes(32);
        const tblob = await collect(t.blob.stream().pipeThrough(encryptStream(tkey)));
        const tid = await send(conv, tblob, (f) => onProgress(0.9 + f * 0.1, 'Uploading the thumbnail'));
        entry.thumb = { id: tid, key: tkey, width: t.tw, height: t.th };
      }
    } catch { /* an image the browser cannot decode goes without one */ }
  }
  onProgress(1, 'Uploaded');
  return fileEntry(entry);
}

async function send(conv, blob, onProgress) {
  const form = new FormData();
  form.append('file', blob, 'sealed');
  const { file } = await upload(`/conversations/${conv}/files`, form, onProgress).promise;
  return file.id;
}

const kindOf = (type) => (/^image\/(png|jpeg|gif|webp)$/.test(type || '') ? 'image' : /^video\//.test(type || '') ? 'video' : /^audio\//.test(type || '') ? 'audio' : 'file');

async function thumbnail(file) {
  if (typeof createImageBitmap !== 'function') return null;
  const bmp = await createImageBitmap(file);
  const scale = Math.min(1, 720 / Math.max(bmp.width, bmp.height));
  const tw = Math.max(1, Math.round(bmp.width * scale)), th = Math.max(1, Math.round(bmp.height * scale));
  const canvas = document.createElement('canvas');
  canvas.width = tw; canvas.height = th;
  canvas.getContext('2d').drawImage(bmp, 0, 0, tw, th);
  const blob = await new Promise((r) => canvas.toBlob(r, 'image/jpeg', 0.85));
  return blob ? { blob, width: bmp.width, height: bmp.height, tw, th } : null;
}

// openFile downloads and decrypts one blob, chunk by chunk. It resolves
// with the plaintext as a Blob only once every chunk has checked out; a
// changed, moved or missing chunk rejects. onProgress gets a fraction.
export async function openFile(id, keyB64, size, onProgress) {
  const key = fromB64(keyB64);
  if (!key || key.length !== 32) throw new Error('This file has no key that could open it.');
  const res = await fetch(`/api/v1/files/${id}/blob`, { credentials: 'same-origin' });
  if (!res.ok) throw new Error(res.status === 404 ? 'This file is no longer on the server.' : 'The file could not be fetched.');
  const want = size != null ? blobSize(size) : 0;
  let body = res.body;
  if (onProgress) body = body.pipeThrough(progressStream((n) => onProgress(want ? Math.min(1, n / want) : 0)));
  return collect(body.pipeThrough(decryptStream(key)));
}

// What the content is, for drawing: an image the browser may show only
// when its first bytes say PNG, JPEG, GIF or WebP, whatever the sender
// said. SVG is always a download.
export async function imageType(blob) {
  const b = new Uint8Array(await blob.slice(0, 12).arrayBuffer());
  if (b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4e && b[3] === 0x47) return 'image/png';
  if (b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff) return 'image/jpeg';
  if (b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46) return 'image/gif';
  if (b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46 && b[8] === 0x57 && b[9] === 0x45 && b[10] === 0x42 && b[11] === 0x50) return 'image/webp';
  return null;
}

// saveBlob hands a decrypted file to the browser to save.
export function saveBlob(blob, name) {
  const url = URL.createObjectURL(new Blob([blob], { type: 'application/octet-stream' }));
  const a = document.createElement('a');
  a.href = url; a.download = name || 'file';
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60000);
}
