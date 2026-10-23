import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const appSource = readFileSync(join(root, 'app.js'), 'utf8');
const loginHTML = readFileSync(join(root, '../login.html'), 'utf8');
const adminSource = readFileSync(join(root, 'admin.js'), 'utf8');

assert.match(loginHTML, /id="login-auth-mode"/, 'login page exposes auth-mode status region');
assert.match(appSource, /explainLoginAuthMode/, 'login explains configured reverse-proxy mode');
assert.match(appSource, /actionableLoginFailure/, 'login surfaces actionable connection failures');
assert.match(appSource, /tryProxySignIn/, 'login probes proxy session without api 401 redirect');
assert.match(appSource, /result\?\.logout_url/, 'logout follows optional IdP logout_url');
assert.match(adminSource, /renderAuthHelp/, 'admin panel documents sign-in / proxy mapping');
assert.match(adminSource, /PROXY_AUTH_TRUSTED_SOURCES/, 'admin help names the trusted-sources setting');

console.log('proxy-auth login/admin UI contracts passed.');
