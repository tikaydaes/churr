// What is inside a sealed message (docs/SEALED.md section 8.3,
// docs/MESSAGE-FORMAT.md section 9): a JSON object the two people's
// devices understand. It is encrypted and signed as bytes by its author
// and only parsed by others, so it needs no canonical form. The builders
// here write the bytes the Go reference does (field order, HTML-safe
// escaping), which is what lets the transcript be reproduced byte for
// byte.
//
// Every reference to another message is that message's reference (the
// identifier its sender made, also its client_id). Bytes are base64url.

import { toB64, utf8 } from './bytes.js';

export const KIND_MESSAGE = 'message';
export const KIND_EDIT = 'edit';
export const KIND_DELETE = 'delete';
export const KIND_REACTION = 'reaction';
export const KIND_TASK = 'task';
export const KIND_HISTORY = 'history';

const HEX = '0123456789abcdef';
const u4 = (c) => '\\u' + HEX[(c >> 12) & 15] + HEX[(c >> 8) & 15] + HEX[(c >> 4) & 15] + HEX[c & 15];

function goString(s) {
  let out = '"';
  for (let i = 0; i < s.length; i++) {
    let c = s.charCodeAt(i);
    // A lone surrogate is written as U+FFFD, as Go does.
    if (c >= 0xd800 && c <= 0xdbff) {
      const d = s.charCodeAt(i + 1);
      if (d >= 0xdc00 && d <= 0xdfff) { out += s[i] + s[i + 1]; i++; continue; }
      out += '�';
      continue;
    }
    if (c >= 0xdc00 && c <= 0xdfff) { out += '�'; continue; }
    switch (c) {
      case 0x22: out += '\\"'; break;
      case 0x5c: out += '\\\\'; break;
      case 0x08: out += '\\b'; break;
      case 0x0c: out += '\\f'; break;
      case 0x0a: out += '\\n'; break;
      case 0x0d: out += '\\r'; break;
      case 0x09: out += '\\t'; break;
      case 0x3c: case 0x3e: case 0x26: case 0x2028: case 0x2029: out += u4(c); break;
      default: out += c < 0x20 ? u4(c) : s[i];
    }
  }
  return out + '"';
}

// goJson writes JSON as Go's encoding/json does: objects in the order
// given, `<`, `>`, `&`, U+2028 and U+2029 escaped.
export function goJson(v) {
  if (v === null || v === undefined) return 'null';
  if (typeof v === 'boolean') return v ? 'true' : 'false';
  if (typeof v === 'number' || typeof v === 'bigint') return String(v);
  if (typeof v === 'string') return goString(v);
  if (Array.isArray(v)) return '[' + v.map(goJson).join(',') + ']';
  return '{' + Object.entries(v).filter(([, x]) => x !== undefined).map(([k, x]) => goString(k) + ':' + goJson(x)).join(',') + '}';
}

const bytes = (v) => utf8(goJson(v));

// fileEntry is a file inside a sealed message: what the server was not
// given. f: { id, key, name, mediaType, kind, size, width, height,
// caption, thumb: { id, key, width, height } }.
export function fileEntry(f) {
  return {
    id: f.id, key: toB64(f.key), name: f.name, media_type: f.mediaType, kind: f.kind, size: f.size,
    width: f.width || undefined, height: f.height || undefined, caption: f.caption || undefined,
    thumb: f.thumb ? { id: f.thumb.id, key: toB64(f.thumb.key), width: f.thumb.width, height: f.thumb.height } : undefined,
  };
}

// A `message`. entities are JSON objects in the order to write; files are
// fileEntry objects; parent a reference; quote { message_id, start, end }.
export function messageContent(text, { entities = [], files = [], parent = null, quote = null } = {}) {
  return bytes({ kind: KIND_MESSAGE, text, entities, files, parent_id: parent, quote });
}

export const editContent = (target, text, entities = []) => bytes({ kind: KIND_EDIT, message_id: target, text, entities });
export const deleteContent = (target) => bytes({ kind: KIND_DELETE, message_id: target });
export const reactionContent = (target, emoji, on) => bytes({ kind: KIND_REACTION, message_id: target, emoji, on });
export const taskContent = (target, item, checked) => bytes({ kind: KIND_TASK, message_id: target, item_id: item, checked });

// A `history`: secrets for epochs the other account had no device in.
export function historyContent(secrets) {
  return bytes({ kind: KIND_HISTORY, secrets: secrets.map((s) => ({ key_epoch: s.keyEpoch, secret: toB64(s.secret) })) });
}
