// Secondary header actions live in one popover menu. The popover gives light
// dismiss and Escape; j/k (and arrows) move between items like the grid.
export function initHeaderMenu() {
  const toggle = document.getElementById('header-menu-toggle');
  const menu = document.getElementById('header-menu');
  if (!toggle || !menu || typeof menu.showPopover !== 'function') return null;
  const items = () => [...menu.querySelectorAll('button')]
    .filter(item => !item.disabled && !item.closest('.hidden') && item.offsetParent !== null);
  const step = delta => {
    const list = items(), at = list.indexOf(document.activeElement);
    list[(at + delta + list.length) % list.length]?.focus();
  };
  menu.addEventListener('toggle', event => {
    const open = event.newState === 'open';
    toggle.setAttribute('aria-expanded', String(open));
    if (open) items()[0]?.focus();
    // Escape or light dismiss can drop focus to <body>; dialogs opened from
    // an item have already taken focus by now and keep it.
    else if (document.activeElement === document.body || menu.contains(document.activeElement)) toggle.focus();
  });
  menu.addEventListener('keydown', event => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === 'j' || event.key === 'ArrowDown') step(1);
    else if (event.key === 'k' || event.key === 'ArrowUp') step(-1);
    else if (event.key === 'Escape') { menu.hidePopover(); toggle.focus(); }
    else if (event.key !== 'Enter' && event.key !== ' ' && event.key !== 'Tab') return;
    if (event.key !== 'Tab') event.stopPropagation();
    if (event.key !== 'Enter' && event.key !== ' ' && event.key !== 'Tab') event.preventDefault();
  });
  menu.addEventListener('click', event => {
    const item = event.target.closest('button');
    // Preview size stays open so repeated steps need one menu visit.
    if (item && !item.closest('.thumbnail-size-controls')) menu.hidePopover();
  });
  return { open: () => menu.showPopover() };
}
