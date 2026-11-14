// Overview: what is running, how big it is, who is connected, the state of
// each outside connection, and warnings.

import { get } from '/shared/api.js';
import { h, bytes, dateTime, duration, ago, icon } from '/shared/dom.js';
import { card } from '/shared/console.js';

const OUTSIDE = {
  certificate: 'Certificate renewal', push_relay: 'Push relay', unifiedpush: 'UnifiedPush', link_previews: 'Link previews',
  object_storage: 'Object storage', identity_provider: 'Identity provider', peers: 'Links to other servers',
};

function uptime(s) {
  if (s < 3600) return duration(Math.max(60, Math.floor(s / 60) * 60));
  if (s < 86400) return duration(Math.floor(s / 3600) * 3600);
  return duration(Math.floor(s / 86400) * 86400);
}

export default async function overview(ctx) {
  const o = (await get('/admin/overview')).overview;
  const fact = (label, value, small) => h('div', {}, h('dt', {}, label), h('dd', {}, value, small && h('small', {}, small)));
  const waiting = [
    [o.waiting.webhooks, 'webhook request', 'webhooks'], [o.waiting.reports, 'report', 'reports'], [o.waiting.purge_requests, 'purge request', 'purge-requests'],
  ].filter(([n]) => n > 0);
  return [
    o.warnings.length ? card('Warnings', h('ul', { class: 'warnings' }, o.warnings.map((w) => h('li', { class: 'note warn' }, icon('warn'), ' ', w)))) : null,
    waiting.length ? card('Waiting for you', h('ul', { class: 'rows' }, waiting.map(([n, what, id]) =>
      h('li', {}, h('div', { class: 'what' }, h('strong', {}, `${n} ${what}${n === 1 ? '' : 's'}`)),
        h('div', { class: 'does' }, h('a', { class: 'btn small', href: '/admin/' + id, onclick: (e) => { e.preventDefault(); ctx.go(id); } }, 'Open')))))) : null,
    card('The server', h('dl', { class: 'facts' },
      fact('Version', o.version),
      fact('Running for', uptime(o.uptime_seconds), 'since ' + dateTime(o.started_at)),
      fact('Database', o.database.kind === 'postgres' ? 'PostgreSQL' : 'SQLite', o.database.size != null ? bytes(o.database.size) : null),
      fact('Files', bytes(o.files.size), `${o.files.count} in ${o.files.store === 's3' ? 'object storage' : 'the data directory'}`),
      fact('Clean-up last ran', o.janitor_last_run_at ? ago(o.janitor_last_run_at) : 'not yet'))),
    card('Right now', h('dl', { class: 'facts' },
      fact('Members online', String(o.members_online)),
      fact('Connected clients', String(o.connections)),
      fact('Live calls', String(o.live_calls), o.media_address ? 'media at ' + o.media_address : null),
      fact('Waiting to send', String(o.outbox), 'to linked servers'))),
    card('Accounts', h('dl', { class: 'facts' },
      fact('People', String(o.accounts.people)), fact('Guests', String(o.accounts.guests)),
      fact('Bots', String(o.accounts.bots)), fact('From other servers', String(o.accounts.remote)))),
    card('Outside connections',
      h('p', { class: 'dim' }, 'Everything this server may reach outside itself. The switches are under Settings, Outside connections.'),
      h('ul', { class: 'rows' }, o.outside.map((c) => h('li', {},
        h('div', { class: 'what' }, h('strong', {}, OUTSIDE[c.name] || c.name),
          h('small', {}, c.last_error ? 'Last problem: ' + c.last_error : c.last_ok_at ? 'Last worked ' + ago(c.last_ok_at) : c.enabled ? 'Not used yet' : 'Never contacted while off')),
        h('span', { class: 'tag ' + (c.state === 'ok' ? 'green' : c.state === 'failing' ? 'red' : 'line') }, c.state))))),
    o.peers.length ? card('Linked servers', h('ul', { class: 'rows' }, o.peers.map((p) => h('li', {},
      h('div', { class: 'what' }, h('strong', {}, p.name || 'Not yet connected'), h('small', {}, p.last_error || (p.connected_at ? 'Connected ' + ago(p.connected_at) : ''))),
      h('span', { class: 'tag ' + (p.state === 'active' ? 'green' : 'line') }, p.state))))) : null,
  ];
}
