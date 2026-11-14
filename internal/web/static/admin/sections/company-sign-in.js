// Company sign-in: add an OIDC or SAML provider, test it, map provider
// groups to the admin role, enable or disable (docs/AUTH.md section 7).

import { get, post, patch, del } from '/shared/api.js';
import { h, button, busy, toast, confirmDialog, copyText, day } from '/shared/dom.js';
import { card } from '/shared/console.js';
import { formDialog } from '/shared/form.js';

function fields(p) {
  const c = (p && p.config) || {};
  const kind = (v) => v.kind;
  return [
    { name: 'kind', label: 'Protocol', type: 'select', value: p ? p.kind : 'oidc', disabled: !!p, options: [['oidc', 'OpenID Connect'], ['saml', 'SAML 2.0']] },
    { name: 'name', label: 'Name on the sign-in button', required: true, value: p && p.name, maxLength: 60, placeholder: 'Example Corp' },
    { name: 'issuer', label: 'Issuer', code: true, value: c.issuer, placeholder: 'https://id.example.com', when: (v) => kind(v) === 'oidc', hint: 'The server reads the discovery document from here.' },
    { name: 'client_id', label: 'Client ID', code: true, value: c.client_id, when: (v) => kind(v) === 'oidc' },
    { name: 'secret', label: 'Client secret', code: true, when: (v) => kind(v) === 'oidc', hint: p && p.has_secret ? 'One is stored. Leave empty to keep it.' : 'Never shown again once saved.' },
    { name: 'metadata_url', label: 'Metadata address', code: true, value: c.metadata_url, when: (v) => kind(v) === 'saml', hint: 'Loaded once, now. Or paste the metadata below.' },
    { name: 'metadata_xml', label: 'Metadata XML', type: 'textarea', value: c.metadata_xml, maxLength: 200000, when: (v) => kind(v) === 'saml' },
    { name: 'group_claim', label: 'Where the provider lists groups', code: true, value: p && p.group_claim, placeholder: 'groups', hint: 'The claim or attribute that holds the person’s groups. Empty if groups are not used.' },
    { name: 'admin_group', label: 'Provider group that makes an admin', code: true, value: p && p.admin_group, hint: 'Checked at each sign-in. Empty leaves roles alone.' },
    { name: 'enabled', label: 'Enabled', type: 'checkbox', value: p ? p.enabled : true, hint: 'A disabled provider has no button and signs nobody in.' },
  ];
}

function body(v, old) {
  const config = { ...((old && old.config) || {}) };
  if (v.kind === 'oidc') { config.issuer = v.issuer; config.client_id = v.client_id; }
  else {
    if (v.metadata_url) config.metadata_url = v.metadata_url; else delete config.metadata_url;
    if (v.metadata_xml) config.metadata_xml = v.metadata_xml; else delete config.metadata_xml;
  }
  const out = { name: v.name, config, group_claim: v.group_claim, admin_group: v.admin_group, enabled: v.enabled };
  if (!old) out.kind = v.kind;
  if (v.secret) out.secret = v.secret;
  return out;
}

export default async function companySignIn(ctx) {
  const { providers } = await get('/admin/sso');
  const add = button('Add a provider', async () => {
    if (await formDialog('Add a provider', fields(null), { wide: true, yes: 'Add', submit: (v) => post('/admin/sso', body(v)) })) ctx.redraw();
  }, 'small primary');
  return card(h('span', { class: 'row grow between' }, 'Providers', add),
    h('p', { class: 'dim' }, 'Each provider is a button on the sign-in page. Adding, changing and removing one needs a fresh check. Mapping a provider group to a Churr group is done on the group, under Groups.'),
    providers.length ? h('ul', { class: 'rows' }, providers.map((p) => {
      const test = button('Test', () => busy(test, async () => {
        const out = await post(`/admin/sso/${p.id}/test`);
        toast(out.ok ? 'The provider answered and its keys were read.' : 'The test failed: ' + (out.error || 'no reason given'), !out.ok);
      }), 'small');
      const edit = button('Change', async () => {
        if (await formDialog('Change ' + p.name, fields(p), { wide: true, submit: (v) => patch('/admin/sso/' + p.id, body({ ...v, kind: p.kind }, p)) })) ctx.redraw();
      }, 'small');
      const remove = button('Remove', async () => {
        if (!(await confirmDialog('Remove this provider?', [`Everyone’s link to ${p.name} is removed with it. Someone who only signs in through it will need a recovery link.`,
          'Refused while members must use company sign-in and this is the last enabled provider.'], { yes: 'Remove', danger: true }))) return;
        await busy(remove, async () => { await del('/admin/sso/' + p.id); ctx.redraw(); });
      }, 'small danger');
      return h('li', {},
        h('div', { class: 'what' },
          h('strong', {}, p.name, ' ', h('span', { class: 'tag line' }, p.kind), ' ', h('span', { class: 'tag ' + (p.enabled ? 'green' : 'grey') }, p.enabled ? 'enabled' : 'disabled')),
          h('small', {}, 'Register this address at the provider: ', h('code', { class: 'wrap' }, p.callback_url), ' ',
            h('button', { type: 'button', class: 'link-btn', onclick: () => copyText(p.callback_url, 'Address copied') }, 'copy')),
          h('small', {}, `Added ${day(p.created_at)}${p.admin_group ? ' · admins from “' + p.admin_group + '”' : ''}${p.group_claim ? ' · groups in “' + p.group_claim + '”' : ''}`)),
        h('div', { class: 'does' }, test, edit, remove));
    })) : h('p', { class: 'empty' }, 'No provider. Members sign in with passkeys.'));
}
