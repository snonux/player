Quick Start
===========

### Running from source

```bash
go build -o player ./cmd/player
./player
```

Open `http://localhost:8080` — on first visit you'll be redirected to the bootstrap page to create an admin account.

### Running with Docker

```bash
docker build -t player:latest .
docker run -p 8080:8080 \
  -v player-data:/data \
  -v /path/to/media:/media \
  -e MEDIA_ROOT=/media \
  -e DB_PATH=/data/media.db \
  player:latest
```

### Deploying to Kubernetes

```bash
kubectl apply -f k8s/
```

This creates a Deployment (non-root, probes included), ClusterIP Service, PVCs for `/data` and `/media`, and an optional Secret.

The `Deployment` overrides two settings for K8s:
- `DB_PATH=/data/media.db`
- `MEDIA_ROOT=/media`

Probes:
- **Liveness:** `GET /healthz` (no DB dependency)
- **Readiness:** `GET /readyz` (DB ping)

Security:
- `runAsNonRoot: true`
- `runAsUser: 65534` / `runAsGroup: 65534`
- `allowPrivilegeEscalation: false`
- `readOnlyRootFilesystem: true`

### Mage targets

| Target | Description |
|--------|-------------|
| `mage build` | Compile the binary |
| `mage test` | Run `go test ./...` |
| `mage install` | Build and copy binary to `$GOPATH/bin` |
| `mage clean` | Remove build artifacts |
| `mage docker-build` | Build Docker image as `player:latest` |
| `mage docker-push` | Push `player:latest` to registry |