#!/usr/bin/env bash
# One-shot deployment of the NP4 bootstrap (DHT seed + mix relay) on a fresh
# Ubuntu 24.04 server, via Docker.
#
#   sudo ./deploy.sh [PUBLIC_IP]
#
# After it finishes, hand the printed multiaddr to clients.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "run with sudo (docker + apt need root)"; exit 1
fi

# --- public IP: argument > external service > first local address -----------
PUBLIC_IP="${1:-}"
if [ -z "$PUBLIC_IP" ]; then
  PUBLIC_IP="$(curl -fsS --max-time 5 https://ifconfig.me 2>/dev/null || true)"
fi
if [ -z "$PUBLIC_IP" ]; then
  PUBLIC_IP="$(hostname -I | awk '{print $1}')"
fi
if [ -z "$PUBLIC_IP" ]; then
  echo "cannot determine the public IP — pass it explicitly: sudo ./deploy.sh 1.2.3.4"
  exit 1
fi
echo "==> public IP: $PUBLIC_IP"

# --- docker (Ubuntu 24.04 repo packages are sufficient) ---------------------
if ! command -v docker >/dev/null 2>&1; then
  echo "==> installing docker.io + docker-compose-v2"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq docker.io docker-compose-v2 curl
  systemctl enable --now docker
fi
if ! docker compose version >/dev/null 2>&1; then
  apt-get install -y -qq docker-compose-v2
fi

# --- firewall: open ONLY the DHT/relay port; dashboard stays private --------
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  echo "==> ufw: allowing 4000/tcp (dashboard deliberately not exposed)"
  ufw allow 4000/tcp
fi

# --- build + start -----------------------------------------------------------
cd "$(dirname "$0")"
echo "==> building image and starting container"
docker compose up -d --build

# --- wait for health ----------------------------------------------------------
echo "==> waiting for the node to become healthy"
status="unknown"
for _ in $(seq 1 60); do
  status="$(docker inspect --format '{{.State.Health.Status}}' np4-bootstrap 2>/dev/null || echo unknown)"
  [ "$status" = "healthy" ] && break
  sleep 2
done
if [ "$status" != "healthy" ]; then
  echo "container not healthy (status=$status) — diagnostics:"
  docker logs --tail 40 np4-bootstrap || true
  exit 1
fi

# --- print the client-facing multiaddr ----------------------------------------
PEER_ID="$(curl -fsS http://127.0.0.1:8080/api/status | python3 -c 'import json,sys; print(json.load(sys.stdin)["peer_id"])')"

cat <<EOF

============================================================
  NP4 bootstrap is up (DHT seed + mix relay).

  Client multiaddr — hand this to np4cli / the apps:

    /ip4/$PUBLIC_IP/tcp/4000/p2p/$PEER_ID

  e.g.
    np4cli --bootstrap /ip4/$PUBLIC_IP/tcp/4000/p2p/$PEER_ID --hops 1 chat

  Dashboard (private by design — use an SSH tunnel):
    ssh -L 8080:127.0.0.1:8080 <server>
    then open http://127.0.0.1:8080

  Identity lives in the 'np4-identity' docker volume. BACK IT UP — a wiped
  volume changes the peer ID and every client's bootstrap address:
    docker run --rm -v np4-identity:/data -v \$PWD:/backup alpine cp /data/boot.id /backup/

  Update later:
    git pull && sudo docker compose up -d --build

  Logs:    docker logs -f np4-bootstrap
  Restart: sudo docker compose restart np4-bootstrap
============================================================
EOF
