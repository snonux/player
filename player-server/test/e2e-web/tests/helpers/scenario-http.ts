import { expect } from '@playwright/test';

export const baseURL = process.env.PLAYER_URL || 'http://127.0.0.1:18081';

export type Session = { cookie: string };

export async function call(
  method: string,
  path: string,
  options: { cookie?: string; bearer?: string; body?: unknown; form?: FormData; raw?: string; headers?: Record<string, string> } = {},
): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json', ...options.headers };
  if (options.cookie) headers.Cookie = options.cookie;
  if (options.bearer) headers.Authorization = `Bearer ${options.bearer}`;
  if (options.body !== undefined) headers['Content-Type'] = 'application/json';
  return fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: options.form ?? options.raw ?? (options.body === undefined ? undefined : JSON.stringify(options.body)),
    signal: AbortSignal.timeout(15000),
    redirect: 'manual',
  });
}

export async function login(username = 'admin', password = 'TestPassw0rd!'): Promise<Session> {
  const response = await call('POST', '/api/v1/auth/login', { body: { username, password } });
  expect(response.status, `login ${username}`).toBe(200);
  const cookie = response.headers.get('set-cookie')?.match(/(?:^|\s)session=([^;]+)/)?.[1];
  expect(cookie, 'login must return a session cookie').toBeTruthy();
  return { cookie: `session=${cookie}` };
}

export async function json<T>(response: Response, status = 200): Promise<T> {
  expect(response.status, `${response.url}: ${response.status === status ? "" : await response.clone().text()}`).toBe(status);
  return response.json() as Promise<T>;
}
