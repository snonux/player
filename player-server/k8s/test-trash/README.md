# Isolated trash test instance

`instance.yaml` creates namespace `player-trash-test`, two **new** local-path
PVCs (`database` and `media`), and `ingress.yaml` separately adds an ingress for
`player-test.f3s.buetow.org`. It never mounts a production claim, NFS directory,
or another application's library. SQLite lives at `/data/media.db`; generated
thumbnails and transcodes share that instance's data volume. The media volume
starts with empty writable sets `trash-tests` and `other-tests`.

`instance.yaml` pins the app image to the approved production release by tag
and digest (currently v0.20.0, `player:021e6fe`, the same image as the
production Player and xplayer). It is not a regression baseline: when
production moves to a new release, update the pin here, re-apply, and rerun
the browser suite below. Deploy or update it with:

```sh
kubectl apply -f k8s/test-trash/instance.yaml
kubectl -n player-trash-test rollout status deployment/player
kubectl -n player-trash-test exec deploy/player -c app -- /app/player --version
```

To test an unreleased build, push it under its own immutable tag (the git
SHA, never a reused tag) and put that tag in `instance.yaml` for the test
run; restore the release pin afterwards. Do not use only `kubectl set image`:
the live deployment then drifts from git, which is how the fixture was left on
0.12.0 while production moved on.

```sh
TAG=$(git rev-parse --short HEAD)
podman build -t registry.lan.buetow.org:30001/player:$TAG .
podman push --tls-verify=false registry.lan.buetow.org:30001/player:$TAG
```

If the controller cannot resolve/reach the LAN registry, forward the registry
service in a separate terminal (`kubectl -n infra port-forward
service/docker-registry-service 15001:5000`) and push the same tag through
that endpoint:

```sh
podman push --tls-verify=false registry.lan.buetow.org:30001/player:$TAG \
  docker://127.0.0.1:15001/player:$TAG
```

The ingress does not create public DNS or a TLS certificate. Those are managed
by the frontend infrastructure in `~/git/conf`; configure this one hostname
there before treating it as a public HTTPS service. Test ingress domains use
`X-test.f3s.buetow.org`; Gogios automatically excludes that suffix. Before
exposing its ingress, temporarily allow HTTP session cookies for a local tunnel:

```sh
kubectl -n player-trash-test set env deployment/player SECURE_COOKIES=false
kubectl -n player-trash-test rollout status deployment/player
kubectl -n player-trash-test port-forward service/player 18083:8080
```

Run the dedicated browser suite from `test/e2e-web/`. It runs in Chrome and
Firefox (`--project chromium` or `--project firefox` picks one). It requires an
explicit opt-in because it uploads and deletes media and creates temporary
accounts:

```sh
PLAYER_TRASH_TEST_INSTANCE=1 PLAYER_URL=http://127.0.0.1:18083 \
  npx playwright test -c playwright.trash.config.ts
```

The suite requires `E2E_ADMIN_USER` and `E2E_ADMIN_PASS` from a local secret
or the test namespace Secret. It bootstraps **only this empty test database**.
Use newly generated credentials, kept outside git. Once bootstrap and local
checks pass, enable Secure cookies and expose only the bootstrapped instance:

```sh
kubectl -n player-trash-test set env deployment/player SECURE_COOKIES=true
kubectl -n player-trash-test rollout status deployment/player
kubectl apply -f k8s/test-trash/ingress.yaml
```

Repeat the browser suite with `PLAYER_URL=https://player-test.f3s.buetow.org`.
Do not run this suite against production. Fixtures remain on the test PVC; they
may be restored and are collected after seven days. Retention-boundary, startup, real filesystem,
rescan, and concurrent restore/GC cases are also exercised by the Go suite
without waiting seven real days.

To rerun with a rebuilt image using the same test database and media, push the
new tag, set it in `instance.yaml` and re-apply; that restarts only this
deployment. To remove the test instance and its
test data, explicitly delete namespace `player-trash-test`; the local-path
PVCs use the storage class's Delete reclaim policy.
