// The approvals queue (docs/FEATURES.md section 31, docs/CLIENTS.md
// section 22): requests that wait for someone who may approve them, at
// /chat/approvals. It is in the chat, not the admin console, since the
// flag "may approve requests" is given to people who are not admins. The
// kinds the reader may decide are its filters; each request is approved,
// or declined with a reason its requester is shown.

import { get, post } from '/shared/api.js';
import { h, icon, iconButton, button, fill, toast, when, avatar, busy, dateTime } from '/shared/dom.js';
import { formDialog } from '/shared/form.js';
import { APPROVAL_KINDS, APPROVAL_KIND_ONE, approvalCount, mayApproveAny, decidedText } from '/shared/roles.js';
import { store, loadApprovals } from './store.js';
import { ui } from './ui.js';

// what says what a request asks for.
function what(a) {
  if (a.kind === 'channel') return `${APPROVAL_KIND_ONE.channel}: #${a.summary || ''}`;
  if (a.kind === 'webhook') return `${APPROVAL_KIND_ONE.webhook}: ${a.summary || ''}`;
  return APPROVAL_KIND_ONE[a.kind] || a.kind;
}

export class ApprovalsView {
  constructor() {
    this.kind = '';
    this.items = null;
    this.problem = null;
    this.filters = h('div', { class: 'tabs', role: 'tablist', 'aria-label': 'Kinds' });
    this.list = h('div', { class: 'results approvals' });
    this.el = h('section', { class: 'view' },
      h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
        h('div', { class: 'conv-title' }, h('h1', {}, icon('check'), ' Approvals')), h('div', { class: 'conv-tools' })),
      h('div', { class: 'view-body' }, this.filters, this.list));
    // A new request, or one decided by someone else, is read again.
    this.off = [store.on('approval', () => this.load()), store.on('approvals', () => this.draw())];
    this.load();
  }

  destroy() { for (const off of this.off) off(); }

  async load() {
    try {
      this.items = (await get('/approvals', this.kind ? { kind: this.kind } : undefined)).approvals;
      this.problem = null;
    } catch (err) { this.problem = err.message; }
    this.draw();
  }

  async decide(a, approve) {
    let reason;
    if (!approve) {
      const v = await formDialog('Decline this request?', [
        { name: 'reason', label: 'Reason (optional)', type: 'textarea', maxLength: 500, rows: 3, hint: 'Shown to them. Who declined is never shown.' },
      ], { yes: 'Decline', danger: true, intro: what(a) + ', from ' + (a.requester ? a.requester.display_name || a.requester.username : 'someone') + '.' });
      if (!v) return;
      reason = (v.reason || '').trim() || undefined;
    }
    try {
      await post(`/approvals/${a.kind}/${encodeURIComponent(a.id)}`, { approve, reason });
      this.items = (this.items || []).filter((x) => !(x.kind === a.kind && x.id === a.id));
      toast(approve ? 'Approved. They are told.' : 'Declined. They are told.');
      this.draw();
      loadApprovals();
    } catch (err) { if (!err.silent) toast(err.message, true); if (err.code === 'not_found') this.load(); }
  }

  draw() {
    const s = store.approvals;
    const kinds = (s && s.kinds) || [];
    const count = (k) => Number(((s && s.counts) || {})[k]) || 0;
    fill(this.filters, [['', 'All', approvalCount(s)], ...kinds.map((k) => [k, APPROVAL_KINDS[k] || k, count(k)])].map(([k, label, n]) =>
      h('button', { type: 'button', role: 'tab', class: 'tab', dataset: { kind: k || 'all' }, 'aria-selected': String(k === this.kind),
        onclick: () => { this.kind = k; this.load(); } }, label, n ? h('span', { class: 'badge', 'aria-label': `${n} waiting` }, String(n)) : null)));
    if (s && !mayApproveAny(s)) { fill(this.list, h('p', { class: 'dim' }, 'You do not decide any requests.')); return; }
    if (this.problem) { fill(this.list, h('p', { class: 'problem' }, this.problem)); return; }
    if (!this.items) { fill(this.list, h('p', { class: 'loading' }, 'Loading')); return; }
    fill(this.list, this.items.length ? this.items.map((a) => {
      const r = a.requester || { id: a.requester_id, username: '…', display_name: 'Someone' };
      return h('article', { class: 'result approval', dataset: { approval: a.id, kind: a.kind } },
        h('div', { class: 'row' }, avatar(r),
          h('div', { class: 'grow' },
            h('strong', {}, h('bdi', {}, r.display_name || r.username)), ' ', h('span', { class: 'dim' }, '@' + r.username),
            h('p', { class: 'small' }, h('span', { class: 'tag line' }, APPROVAL_KINDS[a.kind] || a.kind), ' ', what(a)),
            h('p', { class: 'dim small' }, `Asked ${when(a.created_at)}${a.expires_at ? ' · lapses ' + dateTime(a.expires_at) : ''}`))),
        a.note ? h('blockquote', { class: 'note' }, h('bdi', {}, a.note)) : null,
        h('p', { class: 'row' },
          button('Approve', (e) => busy(e.currentTarget, () => this.decide(a, true)), 'small primary'),
          button('Decline…', () => this.decide(a, false), 'small')));
    }) : h('p', { class: 'dim' }, 'Nothing waits for approval.'));
  }
}

// The live events of the queue: a browser notification of a new request
// for those who decide, following the person's notification settings as
// a message's does; and to the one who asked for a channel, its outcome.
export function watchApprovals() {
  store.on('approval', (e) => {
    const a = e.approval;
    if (!a) return;
    if (e.type === 'approval.decided') {
      if (a.requester_id === store.me.id) toast(decidedText(a), a.state !== 'approved');
      return;
    }
    if (e.type !== 'approval.created' || a.requester_id === store.me.id) return;
    const front = document.visibilityState === 'visible' && document.hasFocus();
    const dnd = store.me.dnd_state && store.me.dnd_state.on;
    if (front || dnd || !('Notification' in window) || Notification.permission !== 'granted' || store.prefs.notify === false) return;
    const who = a.requester ? a.requester.display_name || a.requester.username : 'Someone';
    try {
      const n = new Notification(`${who}: ${what(a).replace(/^./, (x) => x.toLowerCase())}`, { body: 'Waiting for approval', tag: `approval:${a.kind}:${a.id}`, icon: '/icons/icon-192.png' });
      n.onclick = () => { window.focus(); ui.go('/chat/approvals'); n.close(); };
    } catch { /* no notifications outside a service worker on some browsers */ }
  });
}
