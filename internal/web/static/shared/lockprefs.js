// This browser's choices for the lock (docs/CLIENTS.md section 9.27): the
// automatic lock after a time without use, in minutes, 0 for off (the
// default). Kept in this browser only; each device chooses for itself.

export const AUTO_KEY = 'churr-autolock-minutes';

// The choices, in minutes; 0 is off.
export const AUTO_CHOICES = [0, 1, 5, 15, 60];

export function autoMinutes() {
  try {
    const n = Number(localStorage.getItem(AUTO_KEY)) || 0;
    return AUTO_CHOICES.includes(n) ? n : 0;
  } catch { return 0; }
}

export function setAutoMinutes(n) {
  try { localStorage.setItem(AUTO_KEY, String(n)); } catch { /* kept for this page only */ }
}

export const autoLabel = (n) => (n ? `After ${n} minute${n === 1 ? '' : 's'} without use` : 'Off');
