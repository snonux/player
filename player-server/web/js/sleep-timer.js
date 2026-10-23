// Session-only countdown. performance's time domain is unaffected by wall-clock
// adjustments; the absolute deadline can travel to the same-origin popup.
export function createSleepTimer({ now = () => performance.timeOrigin + performance.now(), expire, changed = () => {} }) {
  let deadline = null;
  return {
    start(milliseconds) {
      if (!Number.isFinite(milliseconds) || milliseconds <= 0) return false;
      deadline = now() + milliseconds; changed(); return true;
    },
    restore(value) { deadline = Number.isFinite(value) && value > 0 ? value : null; changed(); },
    cancel() { deadline = null; changed(); },
    deadline: () => deadline,
    remaining: () => deadline === null ? null : Math.max(0, deadline - now()),
    tick() {
      if (deadline === null || now() < deadline) return false;
      deadline = null; changed(); expire(); return true;
    },
  };
}

export function initSleepTimer({ player, expire, ownsSource = () => true }) {
  const controls = player?.querySelector('.controls');
  if (!controls || !document.createElement) return null;
  const row = document.createElement('div'); row.className = 'sleep-timer-control hidden';
  const select = document.createElement('select'); select.setAttribute('aria-label', 'Sleep duration');
  for (const minutes of [1, 5, 15, 30, 45, 60, 90]) {
    const option = document.createElement('option'); option.value = minutes;
    option.textContent = `${minutes} min`; select.appendChild(option);
  }
  select.value = '30';
  const start = document.createElement('button'); start.type = 'button'; start.textContent = 'Start sleep timer';
  const status = document.createElement('span'); status.setAttribute('role', 'timer'); status.setAttribute('aria-label', 'Sleep time remaining');
  const cancel = document.createElement('button'); cancel.type = 'button'; cancel.textContent = 'Cancel timer';
  row.appendChild(select); row.appendChild(start); row.appendChild(status); row.appendChild(cancel); controls.appendChild(row);
  let media = null;
  const timer = createSleepTimer({ expire: () => { if (ownsSource()) expire(); }, changed: render });
  function render() {
    const left = timer.remaining();
    row.classList.toggle('hidden', !media || media.type === 'image');
    cancel.hidden = left === null;
    status.textContent = left === null ? 'Sleep timer off' : `Sleep ${Math.floor(Math.ceil(left / 1000) / 60)}:${String(Math.ceil(left / 1000) % 60).padStart(2, '0')} remaining`;
    start.textContent = left === null ? 'Start sleep timer' : 'Restart sleep timer';
  }
  start.addEventListener('click', () => timer.start(Number(select.value) * 60000));
  cancel.addEventListener('click', () => { timer.cancel(); start.focus(); });
  setInterval(() => { if (ownsSource()) timer.tick(); render(); }, 250);
  document.addEventListener('visibilitychange', () => { if (ownsSource()) timer.tick(); render(); });
  document.addEventListener('keydown', event => {
    if (event.key !== 'Z' || event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.isComposing ||
      event.target?.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(event.target?.tagName) || document.querySelector('.modal-overlay.open') || !media || media.type === 'image') return;
    const panel = row.closest?.('[popover]'); panel?.showPopover?.();
    select.focus(); event.preventDefault();
  });
  render();
  return { ...timer, setMedia(value) { media = value; render(); } };
}
