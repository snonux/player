import { API } from './api.js';
import { escapeHtml, toast } from './utils.js';

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

function renderUsers(users) {
  const el = document.getElementById('admin-users');
  if (!el) return;
  el.innerHTML = `<ul class="admin-list">
    ${users.map((u) => `<li class="${u.is_admin ? 'is-admin' : ''}">
      <span>${escapeHtml(u.username)}${u.is_admin ? ' <span class="badge">admin</span>' : ''}</span>
      <button class="btn btn-danger btn-sm" data-id="${u.id}">Remove</button>
    </li>`).join('')}
  </ul>`;
  el.querySelectorAll('button[data-id]').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await API.deleteUser(b.dataset.id);
        toast('User removed');
        await refreshAdmin();
      } catch (err) {
        const msg = err.message || '';
        if (msg.toLowerCase().includes('cannot delete yourself') || msg.toLowerCase().includes('self-delete')) {
          toast('You cannot delete yourself', 'error');
        } else {
          toast(err.message, 'error');
        }
      }
    });
  });
}

function renderPermissions(data) {
  const el = document.getElementById('admin-permissions');
  if (!el || !data) return;
  if (!data.sets?.length || !data.users?.length) {
    el.innerHTML = '<p class="text-muted text-xs">No sets or users to manage.</p>';
    return;
  }

  // Build a lookup: setId -> userId -> role
  const roleMap = {};
  data.permissions?.forEach((p) => {
    if (!roleMap[p.set_id]) roleMap[p.set_id] = {};
    roleMap[p.set_id][p.user_id] = p.role;
  });

  let html = '<table class="admin-table"><thead><tr><th>Set</th>';
  data.users.forEach((u) => {
    html += `<th>${escapeHtml(u.username)}</th>`;
  });
  html += '</tr></thead><tbody>';

  data.sets.forEach((s) => {
    html += `<tr><td>${escapeHtml(s.name)}</td>`;
    data.users.forEach((u) => {
      const role = roleMap[s.id]?.[u.id] || '';
      const selectId = `perm-${s.id}-${u.id}`;
      html += `<td>
        <select id="${selectId}" class="perm-select" data-set="${s.id}" data-user="${u.id}">
          <option value="" ${!role ? 'selected' : ''}>—</option>
          <option value="viewer" ${role === 'viewer' ? 'selected' : ''}>Viewer</option>
          <option value="owner" ${role === 'owner' ? 'selected' : ''}>Owner</option>
        </select>
      </td>`;
    });
    html += '</tr>';
  });

  html += '</tbody></table>';
  el.innerHTML = html;

  el.querySelectorAll('.perm-select').forEach((sel) => {
    sel.addEventListener('change', async () => {
      const setId = sel.dataset.set;
      const userId = sel.dataset.user;
      const role = sel.value;
      try {
        if (role) {
          await API.setPermissions({ set_id: parseInt(setId), user_id: parseInt(userId), role });
          toast('Permission granted');
        } else {
          await API.delPermissions({ set_id: parseInt(setId), user_id: parseInt(userId) });
          toast('Permission revoked');
        }
      } catch (err) { toast(err.message, 'error'); }
    });
  });
}

function renderTrash(data) {
  const el = document.getElementById('admin-users');
  if (!el || !Array.isArray(data)) return;
  if (!data.length) {
    el.innerHTML = '<h4 class="text-90">Trash</h4><p class="text-muted text-xs">No deleted items.</p>';
    return;
  }
  el.innerHTML = `<h4 class="text-90">Trash</h4><ul class="admin-list">
    ${data.map((m) => `<li>
      <span>${escapeHtml(m.file_name)}</span>
      <span class="text-muted text-75">${escapeHtml(m.deleted_at || '')}</span>
      <button class="btn btn-primary btn-sm" data-id="${m.id}">Restore</button>
    </li>`).join('')}
  </ul>`;

  el.querySelectorAll('button[data-id]').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await API.restore(b.dataset.id);
        toast('Item restored');
        const data = await API.trash();
        renderTrash(data);
      } catch (err) { toast(err.message, 'error'); }
    });
  });
}
