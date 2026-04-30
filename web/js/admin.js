import { API } from './api.js';

export function initAdmin() {
  const btn = document.getElementById('admin-toggle');
  const modal = document.getElementById('admin-modal');
  const closeBtn = document.getElementById('admin-close');
  const rescanBtn = document.getElementById('admin-rescan');
  const trashBtn = document.getElementById('admin-trash');
  const form = document.getElementById('admin-user-form');

  btn?.addEventListener('click', () => {
    modal?.classList.add('open');
    refreshAdmin();
  });
  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

  rescanBtn?.addEventListener('click', async () => {
    rescanBtn.disabled = true;
    try { await API.rescan(); toast('Rescan triggered'); }
    catch (err) { toast(err.message, 'error'); }
    finally { rescanBtn.disabled = false; }
  });

  trashBtn?.addEventListener('click', async () => {
    try {
      const data = await API.trash();
      renderTrash(data);
    } catch (err) { toast(err.message, 'error'); }
  });

  form?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(form);
    const body = {
      username: fd.get('username'),
      password: fd.get('password'),
      is_admin: !!fd.get('is_admin'),
    };
    try {
      await API.createUser(body);
      form.reset();
      toast('User created');
      await refreshAdmin();
    } catch (err) { toast(err.message, 'error'); }
  });
}

async function refreshAdmin() {
  try {
    const [users, perms] = await Promise.all([API.users(), API.permissions()]);
    renderUsers(users);
    renderPermissions(perms);
  } catch (err) { toast(err.message, 'error'); }
}

function esc(s) {
  return (s ?? '').replace(/[&<>"']/g, (c) => ({ '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;' }[c]));
}

function renderUsers(users) {
  const el = document.getElementById('admin-users');
  if (!el) return;
  el.innerHTML = `<ul class="admin-list">
    ${users.map((u) => `<li class="${u.is_admin ? 'is-admin' : ''}">
      <span>${esc(u.username)}${u.is_admin ? ' <span class="badge">admin</span>' : ''}</span>
      <button class="btn btn-danger btn-sm" data-id="${u.id}">Remove</button>
    </li>`).join('')}
  </ul>`;
  el.querySelectorAll('button[data-id]').forEach((b) => {
    b.addEventListener('click', async () => {
      try { await API.deleteUser(b.dataset.id); toast('User removed'); await refreshAdmin(); }
      catch (err) { toast(err.message, 'error'); }
    });
  });
}

function renderPermissions(data) {
  const el = document.getElementById('admin-permissions');
  if (!el || !data) return;
  el.innerHTML = `<p class="text-muted text-xs">Use the API to manage permissions directly.</p>`;
}

function renderTrash(data) {
  const el = document.getElementById('admin-users');
  if (!el || !Array.isArray(data)) return;
  el.innerHTML = `<h4 class="text-90">Trash</h4><ul class="admin-list">
    ${data.map((m) => `<li>
      <span>${esc(m.file_name)}</span>
      <span class="text-muted text-75">${esc(m.deleted_at || '')}</span>
    </li>`).join('')}
  </ul>`;
}

function toast(msg, type = 'info') {
  const t = document.getElementById('toast');
  if (!t) return;
  t.textContent = msg;
  t.className = 'toast show ' + type;
  setTimeout(() => t.classList.remove('show'), 2800);
}
