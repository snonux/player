const key = 'player-download-quality';

export function downloadQuality() {
  try { return localStorage.getItem(key) || 'original'; } catch { return 'original'; }
}

// A native select preserves typing/arrows and existing modal focus behavior.
export function initDownloadQuality(root) {
  const select = root.querySelector('[data-download-quality]');
  const status = root.querySelector('[data-quality-status]');
  if (!select || !status) return;
  let selected = downloadQuality();
  function options(profiles = []) {
    select.replaceChildren();
    const choices = [{id:'original', label:'Original (no conversion)'}, ...profiles];
    if (!choices.some(p => p.id === selected)) choices.push({id:selected, label:`Unavailable profile (${selected})`});
    for (const p of choices) {
      const option = document.createElement('option'); option.value=p.id; option.textContent=p.label; select.append(option);
    }
    select.value = selected;
  }
  async function load() {
    status.textContent = 'Loading download profiles…';
    try {
      const response = await fetch('/api/stream-profiles');
      if (!response.ok) throw Error();
      const profiles = await response.json();
      if (!root.isConnected) return;
      options(profiles); status.textContent='Download profiles ready.';
    } catch { if (root.isConnected) status.textContent='Profiles unavailable. Original remains available; reload to retry.'; }
  }
  options();
  select.onchange = () => {
    try { localStorage.setItem(key, select.value); selected=select.value; status.textContent='Saved for new downloads.'; }
    catch { select.value=selected; status.textContent='Could not save download quality on this browser.'; }
  };
  root.querySelector('[data-quality-reload]').onclick=load;
  load();
}
