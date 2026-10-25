import { globalSyncPresentation } from './global-sync.js';

export function mountGlobalSyncView(host, globalButton, syncNow, state) {
  const text = host.querySelector('[data-sync-status]'), button = host.querySelector('button'), signIn = host.querySelector('a');
  button.addEventListener('click', syncNow);
  globalButton?.addEventListener('click', () => {
    if (state().errorKind === 'auth') { signIn.click(); return; }
    syncNow();
  });
  return (value, now = Date.now()) => {
    const view = globalSyncPresentation(value, now);
    host.classList.toggle('hidden', view.hidden); text.textContent = view.text;
    button.textContent = view.action; button.disabled = !!value.active;
    button.classList.toggle('hidden', value.errorKind === 'auth');
    signIn.classList.toggle('hidden', value.errorKind !== 'auth');
    if (globalButton) {
      globalButton.classList.toggle('hidden', view.hidden);
      globalButton.textContent = view.action; globalButton.disabled = !!value.active;
      globalButton.title = view.text;
    }
  };
}
