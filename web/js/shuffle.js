let shuffleOn = false;

export function initShuffle({ onChange }) {
  const btn = document.getElementById('shuffle-toggle');
  if (!btn) return;
  btn.addEventListener('click', () => {
    shuffleOn = !shuffleOn;
    updateUI();
    onChange?.(shuffleOn);
  });
}

export function toggle() {
  shuffleOn = !shuffleOn;
  updateUI();
  return shuffleOn;
}

export function isOn() { return shuffleOn; }

function updateUI() {
  const btn = document.getElementById('shuffle-toggle');
  if (btn) btn.classList.toggle('active', shuffleOn);
}
