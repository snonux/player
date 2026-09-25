import { API } from './api.js';
import { fmtDateTime } from './dom.js';
import { escapeHtml, toast } from './utils.js';
import { triggerRescan } from './views/admin-status.js';

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
    if (!document.getElementById('admin-trash-section')?.classList.contains('hidden')) refreshTrash();
  });
  closeBtn?.addEventListener('click', () => modal?.classList.remove('open'));
  modal?.addEventListener('click', (e) => { if (e.target === modal) modal.classList.remove('open'); });

  rescanBtn?.addEventListener('click', async () => {
    rescanBtn.disabled = true;
    try { await triggerRescan(); }
    finally {
      rescanBtn.disabled = false;
      // Disabling the focused button dropped focus to <body>; give it back.
      if (document.activeElement === document.body) rescanBtn.focus();
    }
  });

  // Trash has its own section so the user list stays visible; the button
  // toggles it and reloads the items each time it opens.
  trashBtn?.addEventListener('click', async () => {
    const section = document.getElementById('admin-trash-section');
    const open = section?.classList.contains('hidden');
    section?.classList.toggle('hidden', !open);
    trashBtn.setAttribute('aria-expanded', String(Boolean(open)));
    trashBtn.textContent = open ? 'Hide trash' : 'View trash';
    if (open) await refreshTrash();
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
      <button class="btn btn-danger btn-sm" data-id="${u.id}" aria-label="Remove user ${escapeHtml(u.username)}">Remove</button>
    </li>`).join('')}
  </ul>`;
  el.querySelectorAll('button[data-id]').forEach((b) => {
    b.addEventListener('click', async () => {
      // Removing a user also deletes their notes, progress, favorites and
      // shares, so it needs an explicit confirmation.
      const name = users.find((u) => String(u.id) === b.dataset.id)?.username || 'this user';
      if (!confirm(`Remove user "${name}"? Their notes, progress, favorites and shares are deleted too.`)) return;
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
  el.innerHTML = permissionsTableHtml(data);
  el.querySelectorAll('.perm-select').forEach(bindPermissionSelect);
}

// permissionsTableHtml renders one row per set and one role select per user.
function permissionsTableHtml(data) {
  const roleMap = {}; // setId -> userId -> role
  data.permissions?.forEach((p) => {
    if (!roleMap[p.set_id]) roleMap[p.set_id] = {};
    roleMap[p.set_id][p.user_id] = p.role;
  });
  const head = data.users.map((u) => `<th>${escapeHtml(u.username)}</th>`).join('');
  const rows = data.sets.map((s) => {
    const cells = data.users.map((u) => {
      const role = roleMap[s.id]?.[u.id] || '';
      return `<td>
        <select id="perm-${s.id}-${u.id}" class="perm-select" data-set="${s.id}" data-user="${u.id}"
          aria-label="Permission for ${escapeHtml(u.username)} on ${escapeHtml(s.name)}">
          <option value="" ${!role ? 'selected' : ''}>—</option>
          <option value="viewer" ${role === 'viewer' ? 'selected' : ''}>Viewer</option>
          <option value="owner" ${role === 'owner' ? 'selected' : ''}>Owner</option>
        </select>
      </td>`;
    }).join('');
    return `<tr><td>${escapeHtml(s.name)}</td>${cells}</tr>`;
  }).join('');
  return `<table class="admin-table"><thead><tr><th>Set</th>${head}</tr></thead><tbody>${rows}</tbody></table>`;
}

// bindPermissionSelect saves role changes in the order they were made: each
// save waits for the previous one, so quick arrow-key changes cannot reach the
// server out of order. The select stays enabled (disabling it would drop
// keyboard focus); aria-busy marks pending saves. Once the queue drains, the
// select shows the role the server last confirmed, so a failed save is undone
// even when later changes were queued behind it.
function bindPermissionSelect(sel) {
  sel.dataset.saved = sel.value;
  let queue = Promise.resolve();
  let pending = 0;
  sel.addEventListener('change', () => {
    const role = sel.value;
    pending++;
    sel.setAttribute('aria-busy', 'true');
    queue = queue.then(() => savePermission(sel, role)).finally(() => {
      if (--pending > 0) return;
      sel.value = sel.dataset.saved;
      sel.removeAttribute('aria-busy');
    });
  });
}

async function savePermission(sel, role) {
  const body = { set_id: parseInt(sel.dataset.set, 10), user_id: parseInt(sel.dataset.user, 10) };
  try {
    if (role) {
      await API.setPermissions({ ...body, role });
      toast('Permission granted');
    } else {
      await API.delPermissions(body);
      toast('Permission revoked');
    }
    sel.dataset.saved = role;
  } catch (err) {
    toast(err.message || 'Permission change failed', 'error');
  }
}

async function refreshTrash() {
  try {
    renderTrash(await API.trash());
  } catch (err) { toast(err.message || 'Failed to load trash', 'error'); }
}

function renderTrash(data) {
  const el = document.getElementById('admin-trash-list');
  if (!el || !Array.isArray(data)) return;
  if (!data.length) {
    el.innerHTML = '<p class="text-muted text-xs">No deleted items.</p>';
    return;
  }
  el.innerHTML = `<ul class="admin-list">
    ${data.map((m) => `<li>
      <span>${escapeHtml(m.file_name)}</span>
      <span class="text-muted text-75">${escapeHtml(fmtDateTime(m.deleted_at))}</span>
      <button class="btn btn-primary btn-sm" data-id="${m.id}" aria-label="Restore ${escapeHtml(m.file_name)}">Restore</button>
    </li>`).join('')}
  </ul>`;

  el.querySelectorAll('button[data-id]').forEach((b) => {
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await API.restore(b.dataset.id);
        toast('Item restored');
        // Let the library grid show the restored item without a reload.
        document.dispatchEvent(new CustomEvent('library:changed'));
        await refreshTrash();
      } catch (err) {
        b.disabled = false;
        if (document.activeElement === document.body) b.focus();
        toast(err.message, 'error');
      }
    });
  });
}
