/**
 * server.ts — utilities for verifying the Player server is reachable and for
 * setting up test state via the REST API.
 *
 * The Playwright suite does not start the server itself; instead it relies on
 * a server already running at PLAYER_URL (default http://localhost:8080). See
 * the README for instructions on how to start the server before running tests.
 *
 * State-setup helpers (bootstrap, login, createRegularUser) call the API
 * directly with fetch so that Playwright pages stay free of incidental
 * navigation that could interfere with page-level assertions.
 */

const BASE_URL = process.env.PLAYER_URL || 'http://localhost:8080';

/** Credentials used for the admin account created during bootstrap. */
export const ADMIN_USER = 'e2e-admin';
export const ADMIN_PASS = 'e2e-passw0rd!';

/** Credentials for a non-admin user created by the bootstrap helper. */
export const REGULAR_USER = 'e2e-user';
export const REGULAR_PASS = 'e2e-user-passw0rd!';

// -----------------------------------------------------------------------
// Low-level HTTP helpers
// -----------------------------------------------------------------------

/** POST a JSON body to a server endpoint; returns the parsed response. */
async function postJSON(
  path: string,
  body: unknown,
  cookie?: string,
): Promise<{ status: number; body: unknown; cookie?: string }> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (cookie) {
    headers['Cookie'] = cookie;
  }
  const res = await fetch(`${BASE_URL}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
    redirect: 'manual',
  });
  // Extract the session cookie from the Set-Cookie header when present.
  const setCookie = res.headers.get('set-cookie') ?? undefined;
  const sessionMatch = setCookie?.match(/session=([^;]+)/);
  const sessionCookie = sessionMatch ? `session=${sessionMatch[1]}` : undefined;

  let parsed: unknown = null;
  const text = await res.text();
  try {
    parsed = JSON.parse(text);
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed, cookie: sessionCookie };
}

/** GET a server endpoint; returns the parsed response. */
async function getJSON(
  path: string,
  cookie?: string,
): Promise<{ status: number; body: unknown }> {
  const headers: Record<string, string> = {};
  if (cookie) {
    headers['Cookie'] = cookie;
  }
  const res = await fetch(`${BASE_URL}${path}`, { headers, redirect: 'manual' });
  let parsed: unknown = null;
  const text = await res.text();
  try {
    parsed = JSON.parse(text);
  } catch {
    parsed = text;
  }
  return { status: res.status, body: parsed };
}

// -----------------------------------------------------------------------
// Server-state helpers
// -----------------------------------------------------------------------

/**
 * bootstrap calls POST /api/v1/auth/bootstrap to create the first admin
 * account. Safe to call multiple times — if the server is already bootstrapped
 * the call is silently ignored.
 *
 * Returns the session cookie for the admin user so subsequent API calls can
 * reuse the session.
 */
export async function bootstrap(): Promise<string> {
  const res = await postJSON('/api/v1/auth/bootstrap', {
    username: ADMIN_USER,
    password: ADMIN_PASS,
  });
  if (res.status === 200) {
    if (!res.cookie) throw new Error('bootstrap succeeded but no session cookie returned');
    return res.cookie;
  }
  if (res.status === 403) {
    // Already bootstrapped — log in to get a fresh session cookie.
    return login(ADMIN_USER, ADMIN_PASS);
  }
  throw new Error(`bootstrap failed: HTTP ${res.status} – ${JSON.stringify(res.body)}`);
}

/**
 * login authenticates with the given credentials and returns the session cookie.
 */
export async function login(username: string, password: string): Promise<string> {
  const res = await postJSON('/api/v1/auth/login', { username, password });
  if (res.status !== 200) {
    throw new Error(`login failed for ${username}: HTTP ${res.status} – ${JSON.stringify(res.body)}`);
  }
  if (!res.cookie) throw new Error('login succeeded but no session cookie returned');
  return res.cookie;
}

/**
 * ensureRegularUser creates a non-admin user account if it does not already
 * exist. Requires an admin session cookie.
 */
export async function ensureRegularUser(adminCookie: string): Promise<void> {
  // Check current user list — if the regular user already exists, skip creation.
  const list = await getJSON('/api/v1/admin/users', adminCookie);
  const users = list.body as Array<{ username: string }>;
  if (Array.isArray(users) && users.some(u => u.username === REGULAR_USER)) {
    return; // Already exists.
  }
  const res = await postJSON(
    '/api/v1/admin/users',
    { username: REGULAR_USER, password: REGULAR_PASS, is_admin: false },
    adminCookie,
  );
  if (res.status !== 200) {
    throw new Error(`createRegularUser failed: HTTP ${res.status} – ${JSON.stringify(res.body)}`);
  }
}

/**
 * triggerRescan asks the server to scan the media root and returns when at
 * least one set has appeared in the API — or the timeout is exceeded.
 *
 * Sets are created incrementally as each sub-directory is scanned, so we
 * do not need to wait for the full scan to finish; we just need at least
 * one set to be present before running the browse and media-grid tests.
 *
 * The large testmedia library may take several minutes to fully scan on a
 * slow machine, but the first set appears within seconds.
 */
export async function triggerRescan(adminCookie: string, timeoutMs = 30_000): Promise<void> {
  // Check if sets already exist from a previous scan run.
  const existing = await getJSON('/api/v1/sets', adminCookie);
  const existingSets = existing.body as Array<unknown> | null;
  if (Array.isArray(existingSets) && existingSets.length > 0) {
    return; // Already have sets; no rescan needed.
  }

  // POST the rescan request to start scanning.
  const res = await postJSON('/api/v1/admin/rescan', {}, adminCookie);
  if (res.status !== 200) {
    throw new Error(`rescan failed: HTTP ${res.status} – ${JSON.stringify(res.body)}`);
  }

  // Poll until at least one set appears in the API.
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const check = await getJSON('/api/v1/sets', adminCookie);
    const sets = check.body as Array<unknown> | null;
    if (Array.isArray(sets) && sets.length > 0) {
      return; // At least one set is available.
    }
    await new Promise(r => setTimeout(r, 500));
  }
  // Throw so beforeAll fails with a clear message rather than silently continuing
  // into tests that depend on sets and producing confusing assertion errors there.
  throw new Error(
    `triggerRescan: no sets appeared within ${timeoutMs}ms. ` +
      'Ensure the server is started with a non-empty MEDIA_ROOT.',
  );
}

/**
 * waitForServer polls /healthz until the server responds or the timeout is
 * exceeded. Useful when the server is started just before the test run.
 */
export async function waitForServer(timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${BASE_URL}/healthz`);
      if (res.ok) return;
    } catch {
      // Connection refused — server not yet up.
    }
    await new Promise(r => setTimeout(r, 200));
  }
  throw new Error(`Server at ${BASE_URL} did not become healthy within ${timeoutMs}ms`);
}
