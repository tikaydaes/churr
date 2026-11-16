// An event as a calendar file (ruling R73): "Add to calendar" makes it on
// the reader's device, with nothing sent anywhere. RFC 5545, one event,
// times in UTC. testdata/objects/ics.json holds the cases both clients
// pass. No DOM.

// stamp writes an RFC 3339 moment as 20261224T170000Z.
const stamp = (iso) => iso.replace(/\.\d+/, '').replace(/[-:]/g, '');

// text escapes a TEXT value.
const text = (s) => String(s || '').replace(/\\/g, '\\\\').replace(/;/g, '\\;').replace(/,/g, '\\,').replace(/\r?\n/g, '\\n');

// fold breaks a content line into lines of at most 75 octets, each after
// the first starting with a space, never inside a character.
function fold(line) {
  const enc = new TextEncoder();
  const out = [];
  let cur = '', size = 0;
  for (const ch of line) {
    const n = enc.encode(ch).length;
    if (size + n > (out.length ? 74 : 75)) { out.push(cur); cur = ''; size = 0; }
    cur += ch; size += n;
  }
  out.push(cur);
  return out.join('\r\n ');
}

// ics is the file for an event: { uid, start, end, title, link, now }, the
// times as RFC 3339 in UTC.
export function ics({ uid, start, end, title, link, now }) {
  const lines = [
    'BEGIN:VCALENDAR', 'VERSION:2.0', 'PRODID:-//Churr//Churr//EN', 'CALSCALE:GREGORIAN', 'METHOD:PUBLISH',
    'BEGIN:VEVENT',
    'UID:' + text(uid),
    'DTSTAMP:' + stamp(now),
    'DTSTART:' + stamp(start),
    'DTEND:' + stamp(end),
    'SUMMARY:' + text(title),
  ];
  if (link) lines.push('DESCRIPTION:' + text(link), 'URL:' + link);
  lines.push('END:VEVENT', 'END:VCALENDAR');
  return lines.map(fold).join('\r\n') + '\r\n';
}

// eventTitle is the title an event gets: the message's first line, or
// "Event" when it has none.
export function eventTitle(plain) {
  const line = String(plain || '').split('\n').map((l) => l.trim()).find(Boolean) || '';
  return line.length > 200 ? line.slice(0, 199) + '…' : line || 'Event';
}
