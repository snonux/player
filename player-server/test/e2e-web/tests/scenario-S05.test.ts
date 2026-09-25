import { test, expect } from './helpers/scenario-fixture';
import { call, json, login } from './helpers/scenario-http';

type Token = { id: number; name: string; token: string };
type TokenMetadata = { id: number; name: string; token?: string };

test('S05: API token works, can be revoked, and logout invalidates the session', async () => {
  const { cookie } = await login();
  let tokenId: number | undefined;

  try {
    const created = await json<Token>(await call('POST', '/api/v1/auth/tokens', {
      cookie, body: { name: 'e2e-token-lifecycle', expires_in_days: 1 },
    }));
    expect(created.id).toBeGreaterThan(0);
    expect(created.name).toBe('e2e-token-lifecycle');
    expect(created.token).toBeTruthy();
    tokenId = created.id;

    const tokens = await json<TokenMetadata[]>(await call('GET', '/api/v1/auth/tokens', { cookie }));
    const listed = tokens.find(token => token.id === created.id);
    expect(listed?.name).toBe(created.name);
    expect(listed).not.toHaveProperty('token');

    expect((await call('GET', '/api/v1/media?limit=1', { bearer: created.token })).status).toBe(200);

    expect((await call('DELETE', `/api/v1/auth/tokens/${created.id}`, { cookie })).status).toBe(204);
    tokenId = undefined;
    const afterRevoke = await json<TokenMetadata[]>(await call('GET', '/api/v1/auth/tokens', { cookie }));
    expect(afterRevoke.some(token => token.id === created.id)).toBe(false);
    expect((await call('GET', '/api/v1/media?limit=1', { bearer: created.token })).status).toBe(401);

    const logout = await call('POST', '/api/v1/logout', { cookie });
    expect(logout.status).toBe(204);
    expect(logout.headers.get('set-cookie')).toMatch(/session=;/);
    expect((await call('GET', '/api/v1/auth/tokens', { cookie })).status).toBe(401);
  } finally {
    if (tokenId !== undefined) {
      await call('DELETE', `/api/v1/auth/tokens/${tokenId}`, { cookie });
    }
  }
});
