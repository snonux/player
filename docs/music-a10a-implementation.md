# Trusted reverse-proxy authentication (A10a / xj3)

Player optionally maps a trusted proxy's configured identity header to an
existing Player username, exactly; it never provisions accounts. Bearer tokens
and valid session cookies retain precedence. Only the immediate peer address
establishes trust; `X-Forwarded-For` does not. Setup and configuration are in
[player-server/docs/api.md](../player-server/docs/api.md#3-trusted-reverse-proxy-header-optional).

## Interaction and platform scope

The existing login page retains its Player logo, labelled password form and
primary **Sign In** action. A status note above the form explains configured
proxy sign-in and tells an unmapped user to use a password or ask an admin for
a matching account. Invalid credentials appear in the existing alert below
the action; connection failure explains the address/connection check and retry.
Mapped identities bypass the form while preserving the requested destination.
The admin dialog's **Sign-in** help explains exact mapping, trusted-header
configuration, retained password/Bearer support and the configured logout URL.
These are existing screens and theme tokens, with no extra settings screen or
shortcut allocation. Escape returns focus to the admin button.

Web logout follows the optional identity-provider logout URL. The native app
continues to use its existing explicit password login and Bearer tokens, as
documented in the API contract; proxy configuration does not add a native SSO
flow or browser logout redirect. Native logout locks the saved account and
requires another explicit sign-in.

## Integrated acceptance

Source: implementation `f5952f6`, reviewed R1; acceptance source `647c7bb`.
Evidence and private fixture runners are outside git at
`/home/paul/tmp/xj3-acceptance`. Shared fixture `18766` was untouched. The owned
server `18771` trusted only `127.0.0.2/32`; proxy `18773` bound its upstream
socket to that address, stripped client identity/forwarded headers, and
injected an identity selected by private atomic JSON. Local IdP logout
`18774` recorded arrival and removed that identity. No external IdP is claimed.

The parent runner `xj3-api.py` recorded 40 real HTTP checks in
`api-results.json`: both API prefixes, forged direct headers and forwarded
addresses rejected, proxy stripping, missing/unknown/case-mismatched mapping
rejected without account creation, mapped permissions, Bearer/cookie/header
precedence, invalid Bearer rejection, password login, exact range source bytes,
session invalidation and IdP logout.

Installed Google Chrome **144.0.7559.132** and Mozilla Firefox **156.0** passed
the same serial Playwright journey, with default autoplay policy. Each verified
unmapped login guidance, wrong password, network-offline retry, password-cookie
precedence over conflicting proxy identity, mapped admin return destination,
admin help, light/dark 390×844 layouts, Escape/focus, mapped viewer permissions,
IdP logout and post-logout login. Real source media 46 played and produced
about 2.6 seconds of measured 440 Hz PCM in each browser; `p` and Space paused
and resumed, while `/` and typing retained native input behavior.

Command from repository root:

```sh
node player-server/test/e2e-web/node_modules/@playwright/test/cli.js test \
  -c /home/paul/tmp/xj3-acceptance/playwright.config.ts
```

Result: **2 passed (27.1 s)**. The first attempt stopped after successful error
journeys because the runner expected a nonexistent `config.is_admin` property;
it was corrected to require actual admin endpoint access. This was a harness
error; no product change was made. Per-browser `results.json`, screenshots and
`proxy-playback.wav` retain the successful checks.

Native Android **14 / API 34 / x86_64**, `emulator-5554`, used the already
installed release APK **0.8.2 (4022)** from `347e502`, SHA-256
`f47f89fa594097b3df556d1205e7d032190c0e6a8c4cf4a0e03229c938625b6c`.
No APK reinstall or app-data clear was performed. Mapping device port `18771`
to host proxy `18773` let the existing saved server/account go through the real
proxy without creating another profile or queue scope.

The native journey used actual touch controls: wrong-password rejection,
password login through unknown proxy mapping, a real admin-only plugins fetch
while the proxy injected a different non-admin username, media 46 playback,
Back, logout to the locked profile, disconnected login feedback and explicit
reconnect/password retry. Captured QEMU output contains **2.83 seconds at
440 Hz**, with the actual native media session title **M68 Pattern**. Android
uses Bearer tokens throughout its authenticated API and media calls.

Native commands used the existing driver/support modules:

```sh
PYTHONPATH=player-android/test/e2e-live:player-android/integration_test/support \
  python3 /home/paul/tmp/xj3-acceptance/native.py
```

Continuation runners `native-resume.py` and `native-continue.py` preserved
snapshots and successful scenarios while repairing driver-only observations.
The first attempt encountered an old persistent **Added 1 song / Undo** snackbar
from earlier work: dismissing it downward exposed the already queued
`invalid credentials` error; Undo was never invoked. A short scrolling helper
then failed to reach the lower Settings section, and a subsequent oracle
expected “No plugins installed” instead of the actual “No server plugins
installed.” The continuation used the established longer Settings scroll and
exact label. These attempts and their logs are retained; no product fix was
needed. Transient native error messages were observed through semantics waits;
subsequent screenshots may show the form after the snackbar has expired.

The fixture and local IdP test Player's trusted-header and redirect contract,
not a third-party identity provider's configuration. Firefox used pointer
controls through its installed-browser BiDi driver; no Firefox touch-emulation
claim is made. Native audio uses QEMU stream time, not a hardware speaker
measurement. TalkBack and biometric hardware were not exercised.

Final native result: **10 recorded scenarios passed**, including restoration.
The owned account's original queue (media 38, occurrence 1, 2.687 seconds,
“Songs you added”) was restored through public CAS update and native
**Continue session → Replace**, paused. All four preexisting local queue scopes
and saved presentation/theme/audio preferences matched their initial values
exactly. The app returned to original `18766/admin`; adb reverse mappings and
OS night mode matched their initial snapshots, with no active transport.
`android/restoration.json`, `preferences-before.json`, `results.json`,
`versions.json` and continuation logs contain the state evidence. The parent
restored server `18771` to its original configuration (proxy authentication
off, no plugins), PID `3324446`, and stopped the owned proxy and IdP. A fresh
public status request confirmed password/Bearer support and proxy disabled;
both fixture ports were free. See `normal-restored.json`.

## Reused regression gates

This acceptance adds documentation only. Go production code has not changed
since the full race/coverage suite and server build on `c7b6aea`; those passing
gates are reused. The complete web JavaScript suite is retained at
`/home/paul/tmp/wj3-evidence/web-tests.log`. Full Flutter analysis, all **1696**
tests and the release APK build for installed `347e502` passed; logs are
`/home/paul/tmp/wj3-native/back-full-analyze.log`,
`back-full-flutter-tests.log` and `back-apk-build.log`. No unchanged full suite
or APK build was repeated for this documentation/acceptance slice.
