let shuffleOn = false;
let shuffleRevision = 0;

export function initShuffle({ onChange }) {
  const btn = document.getElementById('shuffle-toggle');
  if (!btn) return;
  btn.addEventListener('click', () => {
    shuffleOn = !shuffleOn;
    shuffleRevision += 1;
    updateUI();
    onChange?.(shuffleOn);
  });
}

export function toggle() {
  shuffleOn = !shuffleOn;
  shuffleRevision += 1;
  updateUI();
  return shuffleOn;
}

export function enable() {
  shuffleOn = true;
  shuffleRevision += 1;
  updateUI();
  return shuffleOn;
}

export function isOn() { return shuffleOn; }

export function revision() { return shuffleRevision; }

function updateUI() {
  const btn = document.getElementById('shuffle-toggle');
  if (btn) btn.classList.toggle('active', shuffleOn);
}
