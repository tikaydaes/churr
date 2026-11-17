// Notices (docs/FEATURES.md section 21.13, docs/CLIENTS.md section 9.16):
// what was done that concerns this person, kept for them, at /chat/notices.
// Opening the list marks them all seen; they stay listed.

import { h, icon, iconButton, fill, when, dateTime } from '/shared/dom.js';
import { noticeText } from '/shared/alerts.js';
import { store, nameOf, conversation, seeNotices } from './store.js';
import { ui } from './ui.js';

export class NoticesView {
  constructor() {
    this.list = h('div', { class: 'results notices' });
    this.el = h('section', { class: 'view' },
      h('header', { class: 'conv-head' }, iconButton('back', 'Back to the list', () => ui.go('/chat/'), 'only-compact'),
        h('div', { class: 'conv-title' }, h('h1', {}, icon('info'), ' Notices')), h('div', { class: 'conv-tools' })),
      h('div', { class: 'view-body' },
        h('p', { class: 'dim' }, 'Things done that concern your account, kept for you. Nobody else sees this list.'),
        this.list));
    this.off = [store.on('notices', () => this.draw()), store.on('accounts', () => this.draw())];
    this.draw();
    seeNotices();
  }

  destroy() { for (const off of this.off) off(); }

  draw() {
    const s = store.notices;
    if (!s) { fill(this.list, h('p', { class: 'loading' }, 'Loading')); return; }
    const convName = (id) => { const c = conversation(id); return c ? c.name || '' : ''; };
    fill(this.list, s.list.length ? s.list.map((n) => h('article', { class: 'result notice' + (n.seen_at ? '' : ' unseen'), dataset: { notice: n.id, kind: n.kind } },
      h('p', {}, noticeText(n, nameOf, convName)),
      h('p', { class: 'dim small', title: dateTime(n.created_at) }, when(n.created_at))))
      : h('p', { class: 'dim' }, 'No notices.'));
  }
}
