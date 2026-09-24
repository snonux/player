import { expect } from '@playwright/test';

const baseURL = process.env.PLAYER_URL || 'http://localhost:8080';

export type Session = { cookie: string };

export async function call(
  method: string,
  path: string,
  options: { cookie?: string; bearer?: string; body?: unknown } = {},
): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (options.cookie) headers.Cookie = options.cookie;
  if (options.bearer) headers.Authorization = `Bearer ${options.bearer}`;
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  return fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    redirect: 'manual',
  });
}

export async function login(username = process.env.LLM_E2E_ADMIN_USER || 'admin',
                            password = process.env.LLM_E2E_ADMIN_PASS || 'TestPassw0rd!'): Promise<Session> {
  const response = await call('POST', '/api/v1/auth/login', { body: { username, password } });
  expect(response.status, `login ${username}`).toBe(200);
  const cookie = response.headers.get('set-cookie')?.match(/(?:^|\s)session=([^;]+)/)?.[1];
  expect(cookie, 'login must return a session cookie').toBeTruthy();
  return { cookie: `session=${cookie}` };
}

export async function json<T>(response: Response, status = 200): Promise<T> {
  expect(response.status).toBe(status);
  return response.json() as Promise<T>;
}
