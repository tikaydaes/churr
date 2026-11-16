// The composer keeps the keyboard (docs/CLIENTS.md section 9.5). Typing
// goes to the message box unless another text box has been chosen:
//
// - After sending, the box that sent keeps focus (on phones too).
// - On a desktop (a fine pointer): opening a conversation; a click on a
//   message, the sidebar, a button or empty space; and closing a dialog,
//   menu or picker that a click opened all put focus back in the box. A
//   click that leaves text selected, or lands in a text box, does not.
// - A printable key pressed while focus is on no text box goes to the
//   box: it moves there and the character goes in. Ctrl, Cmd and Alt
//   combinations, keys a focused message or control already uses (R, E,
//   T, P on a message; Space on a control), and anything typed in a
//   dialog or menu are left alone. Pasting outside a text box pastes in
//   the box.
//
// Keyboard use moves focus as it always did: Tab, Shift+Tab and the arrow
// keys go where they go, and a dialog or menu opened from the keyboard
// gives focus back to what opened it. On a phone nothing but sending
// moves focus, so the on-screen keyboard never pops up by itself.
//
// "The box" is whichever composer was used last that can take text now:
// the conversation's or the thread pane's. A locked or read-only
// conversation has none.

const composers = new Set();
let last = null;
let modality = 'keyboard';

const coarse = () => window.matchMedia('(pointer: coarse)').matches;

// register adds a composer: { el, editor, canFocus(), resume(),
// pasteFrom(event) }. It returns a function that removes it.
export function register(c) {
  composers.add(c);
  c.editor.addEventListener('focus', () => { last = c; });
  return () => { composers.delete(c); if (last === c) last = null; };
}

// target is the composer typing should go to, or null.
export function target() {
  const ready = [...composers].filter((c) => c.el.isConnected && c.canFocus() && c.editor.getClientRects().length > 0);
  if (last && ready.includes(last)) return last;
  return ready.find((c) => c.el.closest('#main')) || ready[0] || null;
}

const isComposer = (el) => [...composers].some((c) => c.editor === el || c.editor.contains(el));

// isTextBox says whether an element takes typed text: a text field, a
// text area, a list box (select) or anything editable.
export function isTextBox(el) {
  if (!el || el === document.body) return false;
  if (el.isContentEditable || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT') return true;
  return el.tagName === 'INPUT' && !/^(button|submit|reset|checkbox|radio|image|file|range|color|hidden)$/i.test(el.type);
}

// overlayOpen says whether a dialog, menu or picker is open.
export function overlayOpen() {
  return !!document.querySelector('dialog[open], .menu, .picker');
}

// selecting says whether text outside the composers is selected.
function selecting() {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !sel.rangeCount) return false;
  return !isComposer(sel.anchorNode && sel.anchorNode.nodeType === 1 ? sel.anchorNode : sel.anchorNode && sel.anchorNode.parentNode);
}

// returnFocus puts focus in the composer, unless a text box or an open
// dialog or menu has it, or (after a click) text is selected; with force,
// a text box outside a dialog gives it up too. It returns whether focus
// is in a composer afterwards.
export function returnFocus(c = target(), { force = false, click = false } = {}) {
  if (!c || !c.el.isConnected || !c.canFocus()) return false;
  const a = document.activeElement;
  if (isComposer(a) && !force) return true;
  if ((isTextBox(a) && !force) || overlayOpen() || (click && selecting())) return false;
  c.resume();
  return true;
}

// keepAfterSend gives focus back to the composer that just sent, on any
// device, unless the person has moved on to another text box or a dialog.
export function keepAfterSend(c) {
  if (document.activeElement === c.editor) return;
  returnFocus(c);
}

// opened says a conversation was put on screen: on a desktop its
// composer takes focus. With force (the conversation was just opened, from
// the sidebar's filter or a search result, say) it takes it from a text
// box too. It returns whether it did.
export function opened({ force = false } = {}) {
  if (coarse()) return false;
  return returnFocus(target(), { force });
}

// Space activates a focused control; it goes to the composer only from
// the page itself or a message.
const spaceIsTyping = (a) => !a || a === document.body || a.matches('main, .messages, .conv-scroll, .msg, #main, #thread');

function printable(e) {
  return e.key.length === 1 || e.key === 'Dead' || e.key === 'Process' || e.keyCode === 229;
}

// install starts keeping focus on this page.
export function install() {
  window.addEventListener('pointerdown', () => { modality = 'pointer'; }, true);
  window.addEventListener('keydown', (e) => { if (!printable(e)) modality = 'keyboard'; }, true);

  // A click that was not made from the keyboard (detail 0) puts focus
  // back once whatever it opened has opened, unless it landed in a text
  // box or a control that keeps focus (a list box, a label, a player),
  // or left text selected.
  document.addEventListener('click', (e) => {
    if (coarse() || e.detail === 0) return;
    const t = e.target;
    if (t && t.closest && t.closest('input, textarea, select, label, video, audio, [contenteditable], dialog, .menu, .picker')) return;
    setTimeout(() => returnFocus(target(), { click: true }), 0);
  });

  // A dialog, menu or picker that a click opened gives focus back to the
  // composer when it closes; one opened from the keyboard gives it back to
  // what opened it, as the browser does.
  const overlay = (n) => n.nodeType === 1 && n.matches('dialog, .menu, .picker');
  new MutationObserver((records) => {
    for (const r of records) {
      for (const n of r.addedNodes) if (overlay(n)) n.dataset.byPointer = String(modality === 'pointer');
      for (const n of r.removedNodes) {
        if (overlay(n) && n.dataset.byPointer === 'true' && !coarse()) setTimeout(() => returnFocus(), 0);
      }
    }
  }).observe(document.body, { childList: true });

  // The safety net: a printable key pressed outside any text box goes to
  // the composer. Moving focus during keydown makes the browser put the
  // character there, once; an IME composition starts there.
  document.addEventListener('keydown', (e) => {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.isComposing || !printable(e)) return;
    const a = document.activeElement;
    if (isComposer(a) || isTextBox(a)) return;
    if (overlayOpen() || (a && a.closest && a.closest('dialog, .menu, .picker, [role="dialog"], [role="menu"], [role="listbox"]'))) return;
    if (e.key === ' ' && !spaceIsTyping(a)) return;
    const c = target();
    if (!c) return;
    c.resume();
  });

  // Pasting outside any text box pastes in the composer.
  document.addEventListener('paste', (e) => {
    const a = document.activeElement;
    if (isComposer(a) || isTextBox(a) || overlayOpen()) return;
    const c = target();
    if (!c) return;
    c.resume();
    c.pasteFrom(e);
  });
}
