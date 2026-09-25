// modal-focus.js — keyboard focus for dialogs (.modal-overlay).
//
// Dialogs open and close by toggling the "open" class from many modules.
// Instead of repeating focus code in each one, this module watches them:
// - opening moves focus into the dialog, unless the opener already did;
// - Tab and Shift+Tab cycle inside the open dialog;
// - when a dialog re-renders its content (e.g. after Restore or Remove) and
//   the focused control disappears, focus moves to the control now at the
//   same position, so keyboard users keep their place;
// - closing returns focus to the element focused before the dialog opened,
//   so vi shortcuts continue from there.
import { focusSelectedCard } from './selection.js';

const FOCUSABLE = [
  'button:not([disabled])', 'a[href]', 'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])', 'textarea:not([disabled])', '[tabindex]:not([tabindex="-1"])',
].join(',');
const TEXT_FIELDS = 'input:not([type="checkbox"]):not([type="radio"]):not([type="file"]), textarea, select';
const HISTORY_SIZE = 20;

// Recently focused elements, newest last. Openers call focus() inside the
// dialog before the MutationObserver runs, so the opener is looked up here
// instead of reading document.activeElement at observation time.
const focusHistory = [];
const openers = new WeakMap();
const lastIndex = new WeakMap();

export function initModalFocus() {
  document.addEventListener('focusin', recordFocus, true);
  document.addEventListener('keydown', trapTab, true);
  document.querySelectorAll('.modal-overlay').forEach((modal) => {
    modal.dataset.focusOpen = String(modal.classList.contains('open'));
    new MutationObserver((records) => onMutation(modal, records)).observe(modal, {
      attributes: true, attributeFilter: ['class'], childList: true, subtree: true,
    });
  });
}

function recordFocus(e) {
  focusHistory.push(e.target);
  if (focusHistory.length > HISTORY_SIZE) focusHistory.shift();
  const modal = e.target.closest?.('.modal-overlay.open');
  if (modal) lastIndex.set(modal, focusables(modal).indexOf(e.target));
}

function onMutation(modal, records) {
  if (records.some((r) => r.type === 'attributes')) onToggle(modal);
  if (records.some((r) => r.type === 'childList')) keepFocusInside(modal);
}

function onToggle(modal) {
  const open = modal.classList.contains('open');
  if (String(open) === modal.dataset.focusOpen) return;
  modal.dataset.focusOpen = String(open);
  if (open) {
    openers.set(modal, lastFocusedOutside(modal));
    if (!modal.contains(document.activeElement)) initialTarget(modal)?.focus();
  } else {
    restoreFocus(modal);
  }
}

// lastFocusedOutside returns the newest focused element outside modal that is
// still in the page (for a stacked dialog, a control in the dialog below).
function lastFocusedOutside(modal) {
  for (let i = focusHistory.length - 1; i >= 0; i--) {
    const el = focusHistory[i];
    if (el.isConnected && !modal.contains(el)) return el;
  }
  return null;
}

// initialTarget prefers an explicit [autofocus], then the first form field,
// then the first action button, and only then the header close button.
function initialTarget(modal) {
  const items = focusables(modal);
  return modal.querySelector('[autofocus]')
    || items.find((el) => el.matches(TEXT_FIELDS))
    || items.find((el) => !el.closest('.modal-header'))
    || items[0];
}

// keepFocusInside refocuses the control at the previous position when a
// re-render removed the focused control and focus fell back to <body>.
function keepFocusInside(modal) {
  if (!modal.classList.contains('open') || document.activeElement !== document.body) return;
  const items = focusables(modal);
  if (!items.length) return;
  const index = Math.min(Math.max(lastIndex.get(modal) ?? 0, 0), items.length - 1);
  items[index].focus();
}

function restoreFocus(modal) {
  const active = document.activeElement;
  // Leave focus alone if something already moved it somewhere useful.
  if (active && active !== document.body && !modal.contains(active)) return;
  const opener = openers.get(modal);
  openers.delete(modal);
  if (isFocusable(opener)) {
    opener.focus();
    return;
  }
  focusSelectedCard();
}

function isFocusable(el) {
  return Boolean(el && el !== document.body && el.isConnected && el.getClientRects().length
    && !el.closest('.modal-overlay:not(.open)'));
}

function focusables(modal) {
  return Array.from(modal.querySelectorAll(FOCUSABLE)).filter((el) => el.getClientRects().length);
}

// trapTab keeps Tab inside the dialog that holds focus (or the last opened
// one), as expected for aria-modal dialogs.
function trapTab(e) {
  if (e.key !== 'Tab' || e.ctrlKey || e.altKey || e.metaKey) return;
  const open = Array.from(document.querySelectorAll('.modal-overlay.open'));
  if (!open.length) return;
  const modal = open.find((m) => m.contains(document.activeElement)) || open[open.length - 1];
  const items = focusables(modal);
  if (!items.length) return;
  const index = items.indexOf(document.activeElement);
  const next = e.shiftKey
    ? items[index <= 0 ? items.length - 1 : index - 1]
    : items[index < 0 || index === items.length - 1 ? 0 : index + 1];
  e.preventDefault();
  next.focus();
}
