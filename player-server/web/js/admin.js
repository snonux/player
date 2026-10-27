import { API } from './api.js';
import { fmtDateTime } from './dom.js';
import { escapeHtml, toast } from './utils.js';
import { triggerRescan, refreshScanProgress } from './views/admin-status.js';

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
    const returnFocus = document.activeElement === rescanBtn;
    rescanBtn.disabled = true;
    try { await triggerRescan(); }
    finally {
      rescanBtn.disabled = false;
      // A modal's focus trap can move focus into the form while this button
      // is disabled; return keyboard users to the action they activated.
      if (returnFocus && modal?.classList.contains('open')) rescanBtn.focus();
    }
  });

  document.getElementById('admin-scan-cancel')?.addEventListener('click', async () => {
    try { await API.cancelScan(); await refreshScanProgress(); }
    catch (err) { toast(err.message || 'Could not cancel scan', 'error'); }
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

  const pluginsBtn = document.getElementById('admin-plugins-toggle');
  pluginsBtn?.addEventListener('click', async () => {
    const section = document.getElementById('admin-plugins-section');
    const open = section?.classList.contains('hidden');
    section?.classList.toggle('hidden', !open);
    pluginsBtn.setAttribute('aria-expanded', String(Boolean(open)));
    pluginsBtn.textContent = open ? 'Hide plugins' : 'Plugins';
    if (open) await refreshPlugins();
  });

  const librariesBtn = document.getElementById('admin-libraries-toggle');
  librariesBtn?.addEventListener('click', async () => {
    const section = document.getElementById('admin-libraries-section');
    const open = section?.classList.contains('hidden');
    section?.classList.toggle('hidden', !open);
    librariesBtn.setAttribute('aria-expanded', String(Boolean(open)));
    librariesBtn.textContent = open ? 'Hide libraries' : 'Libraries';
    if (open) await refreshLibraries();
  });

  document.getElementById('admin-library-form')?.addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    try {
      await API.createLibrary({ name: fd.get('name'), root_path: fd.get('root_path') });
      e.target.reset();
      toast('Library added');
      await refreshLibraries();
      await refreshAdmin();
    } catch (err) { toast(err.message || 'Could not add library', 'error'); }
  });

  const identityBtn = document.getElementById('admin-identity-toggle');
  identityBtn?.addEventListener('click', async () => {
    const section = document.getElementById('admin-identity-section');
    const open = section?.classList.contains('hidden');
    section?.classList.toggle('hidden', !open);
    identityBtn.setAttribute('aria-expanded', String(Boolean(open)));
    identityBtn.textContent = open ? 'Hide music identity' : 'Music identity';
    if (open) await refreshIdentityConflicts();
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
    const [users, perms, cfg] = await Promise.all([API.users(), API.permissions(), API.config()]);
    renderUsers(users);
    renderPermissions(perms);
    renderAuthHelp(cfg?.auth, cfg?.subsonic);
  } catch (err) { toast(err.message, 'error'); }
}

function renderAuthHelp(auth, subsonic) {
  const el = document.getElementById('admin-auth-help');
  if (!el) return;
  if (!auth) {
    el.textContent = 'Authentication settings are unavailable.';
    return;
  }
  let text;
  if (!auth.proxy) {
    text = 'Sign-in modes: password and Bearer API tokens. Reverse-proxy authentication is off (set PROXY_AUTH_TRUSTED_SOURCES to enable). Create Player usernames that match IdP users before enabling proxy mapping.';
  } else {
    const header = auth.proxy_auth?.user_header || 'Remote-User';
    const logout = auth.proxy_auth?.logout_url
      ? ` Logout redirects to ${auth.proxy_auth.logout_url}.`
      : ' No PROXY_AUTH_LOGOUT_URL configured — proxy sessions may return immediately after logout.';
    text = `Reverse-proxy authentication is on. Trusted proxies may set ${header} to a Player username (exact match; accounts are not auto-created). Password and Bearer token sign-in still work. Untrusted clients cannot spoof ${header}.${logout}`;
  }
  if (subsonic?.enabled) {
    text += ` Subsonic/OpenSubsonic is on at ${subsonic.base_path || '/rest'} (API ${subsonic.api_version || '1.16.1'}). External clients use username+password or an API token as apiKey; revoke tokens in Settings › API Tokens. Credentials are never shown here.`;
  } else {
    text += ' Subsonic compatibility is off (set SUBSONIC_ENABLED=true to enable /rest for DSub/Tempo).';
  }
  el.textContent = text;
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

async function refreshPlugins() {
  const status = document.getElementById('admin-plugins-status');
  const el = document.getElementById('admin-plugins');
  if (status) status.textContent = 'Loading plugins…';
  try {
    const items = await API.plugins();
    renderPlugins(items);
    if (status) {
      status.textContent = items.length
        ? `${items.length} plugin${items.length === 1 ? '' : 's'} on this server.`
        : 'No plugins installed.';
    }
  } catch (err) {
    if (status) status.textContent = '';
    if (el) el.innerHTML = `<p class="text-muted text-xs">Could not load plugins: ${escapeHtml(err.message || 'error')}</p>`;
    toast(err.message || 'Failed to load plugins', 'error');
  }
}

function renderPlugins(items) {
  const el = document.getElementById('admin-plugins');
  if (!el) return;
  if (!Array.isArray(items) || !items.length) {
    el.innerHTML = '<p class="text-muted text-xs">Place a plugin folder under the server plugin directory to get started.</p>';
    return;
  }
  el.innerHTML = `<ul class="admin-list">
    ${items.map((p) => pluginRowHtml(p)).join('')}
  </ul>`;
  el.querySelectorAll('[data-plugin-action]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.dataset.id;
      const action = btn.dataset.pluginAction;
      if (action === 'disable' && !confirm(`Disable plugin "${id}"? Music playback is unaffected.`)) {
        return;
      }
      btn.disabled = true;
      try {
        if (action === 'enable') {
          await API.enablePlugin(id);
          toast('Plugin enabled');
        } else if (action === 'disable') {
          await API.disablePlugin(id);
          toast('Plugin disabled');
        } else if (action === 'probe') {
          const res = await API.invokePlugin(id, 'probe');
          toast(`Probe result: ${res.result}`);
        }
        await refreshPlugins();
      } catch (err) {
        toast(err.message || 'Plugin action failed', 'error');
        await refreshPlugins();
      } finally {
        btn.disabled = false;
        if (document.activeElement === document.body) btn.focus();
      }
    });
  });
}

function pluginRowHtml(p) {
  const status = escapeHtml(p.enabled && p.status === 'disabled'
    ? 'enabled (ready for probe)' : (p.status || 'disabled'));
  const caps = (p.capabilities || []).map((c) => escapeHtml(c)).join(', ') || '—';
  const err = p.error ? `<span class="text-muted text-75"> — ${escapeHtml(p.error)}</span>` : '';
  const enable = p.enabled
    ? `<button type="button" class="btn btn-danger btn-sm" data-plugin-action="disable" data-id="${escapeHtml(p.id)}" aria-label="Disable ${escapeHtml(p.name)}">Disable</button>`
    : `<button type="button" class="btn btn-primary btn-sm" data-plugin-action="enable" data-id="${escapeHtml(p.id)}" aria-label="Enable ${escapeHtml(p.name)}">Enable</button>`;
  const probe = p.enabled && (p.capabilities || []).includes('probe')
    ? `<button type="button" class="btn btn-sm" data-plugin-action="probe" data-id="${escapeHtml(p.id)}" aria-label="Run probe on ${escapeHtml(p.name)}">Run probe</button>`
    : '';
  return `<li>
    <span>
      <strong>${escapeHtml(p.name || p.id)}</strong>
      <span class="text-muted text-75"> v${escapeHtml(p.version || '?')} · ${status}${err}</span>
      <br><span class="text-muted text-75">Capabilities: ${caps}</span>
    </span>
    <span class="flex gap-1">${enable}${probe}</span>
  </li>`;
}

async function refreshLibraries() {
  const status = document.getElementById('admin-libraries-status');
  const el = document.getElementById('admin-libraries');
  if (status) status.textContent = 'Loading libraries…';
  try {
    const [libs, perms] = await Promise.all([API.adminLibraries(), API.permissions()]);
    renderLibraries(libs, perms);
    if (status) {
      status.textContent = libs.length
        ? `${libs.length} librar${libs.length === 1 ? 'y' : 'ies'} configured.`
        : 'No libraries yet.';
    }
  } catch (err) {
    if (status) status.textContent = '';
    if (el) el.innerHTML = `<p class="text-muted text-xs">Could not load libraries: ${escapeHtml(err.message || 'error')}</p>`;
    toast(err.message || 'Failed to load libraries', 'error');
  }
}

function renderLibraries(libs, perms) {
  const el = document.getElementById('admin-libraries');
  if (!el) return;
  if (!Array.isArray(libs) || !libs.length) {
    el.innerHTML = '<p class="text-muted text-xs">Add a filesystem root to create another music library.</p>';
    return;
  }
  const users = perms?.users || [];
  el.innerHTML = `<ul class="admin-list">${libs.map((lib) => libraryRowHtml(lib, users)).join('')}</ul>`;
  el.querySelectorAll('[data-library-rename]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const name = prompt('Library name', btn.dataset.name || '');
      if (!name) return;
      try {
        await API.renameLibrary(btn.dataset.id, { name });
        toast('Library renamed');
        await refreshLibraries();
      } catch (err) { toast(err.message || 'Rename failed', 'error'); }
    });
  });
  el.querySelectorAll('[data-library-delete]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!confirm(`Remove library "${btn.dataset.name}"? It must have no sets.`)) return;
      try {
        await API.deleteLibrary(btn.dataset.id);
        toast('Library removed');
        await refreshLibraries();
        await refreshAdmin();
      } catch (err) { toast(err.message || 'Remove failed', 'error'); }
    });
  });
  el.querySelectorAll('[data-library-access]').forEach((sel) => {
    sel.addEventListener('change', async () => {
      const body = { user_id: parseInt(sel.dataset.user, 10) };
      sel.disabled = true;
      try {
        if (sel.value) {
          await API.grantLibraryAccess(sel.dataset.id, { ...body, role: sel.value });
          toast('Library access granted');
        } else {
          await API.revokeLibraryAccess(sel.dataset.id, body);
          toast('Library access revoked');
        }
      } catch (err) {
        toast(err.message || 'Access change failed', 'error');
        await refreshLibraries();
      } finally {
        sel.disabled = false;
      }
    });
  });
}

function libraryRowHtml(lib, users) {
  const stats = lib.stats || {};
  const access = users.map((u) => `
    <label class="text-xs">${escapeHtml(u.username)}
      <select class="perm-select" data-library-access data-id="${lib.id}" data-user="${u.id}"
        aria-label="Access for ${escapeHtml(u.username)} on ${escapeHtml(lib.name)}">
        <option value="">—</option>
        <option value="viewer">Viewer</option>
        <option value="owner">Owner</option>
      </select>
    </label>`).join('');
  return `<li>
    <span>
      <strong>${escapeHtml(lib.name)}</strong>
      <span class="text-muted text-75"> · ${escapeHtml(lib.root_path)}</span>
      <br><span class="text-muted text-75">${stats.set_count || 0} sets · ${stats.song_count || 0} songs</span>
      <div class="flex gap-2 flex-wrap mt-1">${access}</div>
    </span>
    <span class="flex gap-1">
      <button type="button" class="btn btn-sm" data-library-rename data-id="${lib.id}" data-name="${escapeHtml(lib.name)}" aria-label="Rename ${escapeHtml(lib.name)}">Rename</button>
      <button type="button" class="btn btn-danger btn-sm" data-library-delete data-id="${lib.id}" data-name="${escapeHtml(lib.name)}" aria-label="Remove ${escapeHtml(lib.name)}">Remove</button>
    </span>
  </li>`;
}

async function refreshIdentityConflicts() {
  const status = document.getElementById('admin-identity-status');
  const el = document.getElementById('admin-identity-conflicts');
  if (status) status.textContent = 'Loading identity conflicts…';
  try {
    const list = await API.musicIdentityConflicts();
    renderIdentityConflicts(list);
    if (status) {
      status.textContent = list.length
        ? `${list.length} conflict${list.length === 1 ? '' : 's'} need a choice`
        : 'No identity conflicts. Moved songs keep playlists and history automatically.';
    }
  } catch (err) {
    if (status) status.textContent = err.message || 'Could not load conflicts';
  }
}

function renderIdentityConflicts(list) {
  const el = document.getElementById('admin-identity-conflicts');
  if (!el) return;
  if (!Array.isArray(list) || !list.length) {
    el.innerHTML = '';
    return;
  }
  el.innerHTML = `<ul class="admin-list">${list.map((c) => {
    const paths = (c.candidate_paths || []).map((p) => escapeHtml(p)).join(', ') || 'unknown path';
    const ids = (() => {
      try { return JSON.parse(c.candidate_media_ids || '[]'); } catch { return []; }
    })();
    const mergeBtns = ids.map((id, i) =>
      `<button type="button" class="btn btn-primary btn-sm" data-identity-merge data-conflict="${c.id}" data-candidate="${id}"
        aria-label="Merge into older song using candidate ${i + 1}">Merge → ${escapeHtml((c.candidate_paths || [])[i] || String(id))}</button>`
    ).join(' ');
    return `<li>
      <span>
        <strong>${escapeHtml(c.missing_path || ('#' + c.missing_media_id))}</strong>
        <br><span class="text-muted text-75">Candidates: ${paths}</span>
        <br><span class="text-muted text-75">${escapeHtml(c.reason || '')}</span>
      </span>
      <span class="flex gap-1 flex-wrap">
        <button type="button" class="btn btn-sm" data-identity-keep data-conflict="${c.id}" aria-label="Keep separate">Keep separate</button>
        ${mergeBtns}
      </span>
    </li>`;
  }).join('')}</ul>`;

  el.querySelectorAll('[data-identity-keep]').forEach((b) => {
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await API.resolveMusicIdentityConflict(b.dataset.conflict, { action: 'keep_separate' });
        toast('Kept separate');
        await refreshIdentityConflicts();
      } catch (err) {
        b.disabled = false;
        toast(err.message || 'Resolve failed', 'error');
      }
    });
  });
  el.querySelectorAll('[data-identity-merge]').forEach((b) => {
    b.addEventListener('click', async () => {
      b.disabled = true;
      try {
        await API.resolveMusicIdentityConflict(b.dataset.conflict, {
          action: 'merge',
          candidate_media_id: Number(b.dataset.candidate),
        });
        toast('Merged — playlists and history kept on the older song');
        await refreshIdentityConflicts();
        document.dispatchEvent(new CustomEvent('library:changed'));
      } catch (err) {
        b.disabled = false;
        toast(err.message || 'Merge failed', 'error');
      }
    });
  });
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
