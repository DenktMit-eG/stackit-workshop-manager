#!/usr/bin/env bash
# Routes STACKIT audit logs of a project into an Observability instance (Loki) via the
# Telemetry Router + Telemetry Link services, using the STACKIT CLI's authenticated `stackit curl`.
#
# Usage: scripts/telemetry-setup.sh <project-id> <observability-instance-id> [otlp-logs-ingest-url]
#   The OTLP ingest URL is shown in the Portal under the Observability instance > API > Logs, e.g.
#   https://logs.stackit17.argus.eu01.stackit.cloud/instances/<instance-id>/otlp/v1/logs
#   If omitted, the script looks for it in `stackit observability instance describe`.
#
# Env:  STACKIT=stackit   path of the STACKIT CLI (>= 0.60, needs `curl` and `observability` commands)
#       REGION=eu01       region of router and link (one link per region and project)
#       SA_KEY=<file>     service account key JSON; when set, it is activated first
#       NAME=ws-telemetry display name used for router, token, destination and link
#
# The service account / user needs on the project: Telemetry Router admin, Telemetry Link admin and
# Observability admin (or simply owner). Steps already done are reused on re-runs (router, destination
# and link are looked up by display name); the access token and the observability credentials are
# created fresh each run.
set -euo pipefail

P=${1:?project id}; OBS=${2:?observability instance id}; OTLP=${3:-}
STACKIT=${STACKIT:-stackit}; REGION=${REGION:-eu01}; NAME=${NAME:-ws-telemetry}
ROUTERS="https://telemetry-router.api.stackit.cloud/v1/projects/$P/regions/$REGION/telemetry-routers"
LINK="https://telemetry-link.api.stackit.cloud/v1/projects/$P/regions/$REGION/telemetry-link"

log() { printf '%s %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
# py <expr>: evaluate a python expression over the JSON document on stdin (bound to d)
py() { python3 -c "import json,sys; d=json.load(sys.stdin); r=($1); print(r if isinstance(r,str) else json.dumps(r))"; }
# find <key-substring>: first string value whose key contains the substring, anywhere in the JSON
find_key() { python3 -c '
import json,sys
want=sys.argv[1].lower()
def walk(x):
    if isinstance(x,dict):
        for k,v in x.items():
            if want in k.lower() and isinstance(v,str): return v
        for v in x.values():
            r=walk(v)
            if r: return r
    if isinstance(x,list):
        for v in x:
            r=walk(v)
            if r: return r
walk_result=walk(json.load(sys.stdin)); print(walk_result or "")' "$1"; }
sc() { "$STACKIT" curl -H "Content-Type: application/json" "$@"; }

if [ -n "${SA_KEY:-}" ]; then
  "$STACKIT" auth activate-service-account --service-account-key-path "$SA_KEY" >/dev/null
fi
"$STACKIT" config set --project-id "$P" >/dev/null

# ---- 1. telemetry router -------------------------------------------------------------------------
log "telemetry router: looking for '$NAME'"
ROUTER=$(sc "$ROUTERS" | py "next((r['id'] for r in (d.get('items') or d.get('telemetryRouters') or (d if isinstance(d,list) else [])) if r.get('displayName')=='$NAME'), '')")
if [ -z "$ROUTER" ]; then
  log "creating telemetry router"
  ROUTER=$(sc -X POST "$ROUTERS" --data "{\"displayName\":\"$NAME\",\"description\":\"audit logs -> observability $OBS\"}" | py "d['id']")
fi
log "router id: $ROUTER"
for _ in $(seq 1 60); do
  STATE=$(sc "$ROUTERS/$ROUTER" | find_key status)
  log "router state: ${STATE:-?}"
  case "${STATE,,}" in active|ready|running) break;; esac
  sleep 10
done

# ---- 2. access token (used by the telemetry link to push into the router) -------------------------
log "creating access token"
TOKEN_JSON=$(sc -X POST "$ROUTERS/$ROUTER/access-tokens" --data "{\"ttl\":null,\"displayName\":\"$NAME-link\",\"description\":\"used by the telemetry link\"}")
TOKEN=$(printf '%s' "$TOKEN_JSON" | find_key token)
[ -n "$TOKEN" ] || { echo "no token in response: $TOKEN_JSON" >&2; exit 1; }

# ---- 3. observability credentials + OTLP ingest URL ------------------------------------------------
if [ -z "$OTLP" ]; then
  OTLP=$("$STACKIT" observability instance describe "$OBS" --project-id "$P" -o json | find_key otlp || true)
  [ -n "$OTLP" ] || { echo "OTLP ingest URL not found in instance details; pass it as 3rd argument" >&2; exit 1; }
fi
log "OTLP ingest: $OTLP"
log "creating observability credentials (technical user)"
CRED=$("$STACKIT" observability credentials create --instance-id "$OBS" --project-id "$P" -y -o json)
OBS_USER=$(printf '%s' "$CRED" | find_key username); OBS_PASS=$(printf '%s' "$CRED" | find_key password)
[ -n "$OBS_USER" ] && [ -n "$OBS_PASS" ] || { echo "no username/password in: $CRED" >&2; exit 1; }

# ---- 4. destination: OTLP with basic auth ---------------------------------------------------------
DEST=$(sc "$ROUTERS/$ROUTER/destinations" | py "next((x['id'] for x in (d.get('items') or d.get('destinations') or (d if isinstance(d,list) else [])) if x.get('displayName')=='$NAME'), '')")
BODY=$(python3 -c 'import json,sys; print(json.dumps({"displayName":sys.argv[1],"description":"STACKIT Observability "+sys.argv[2],"config":{"configType":"OpenTelemetry","openTelemetry":{"uri":sys.argv[3],"basicAuth":{"username":sys.argv[4],"password":sys.argv[5]}}}}))' "$NAME" "$OBS" "$OTLP" "$OBS_USER" "$OBS_PASS")
if [ -z "$DEST" ]; then
  log "creating destination"
  DEST=$(sc -X POST "$ROUTERS/$ROUTER/destinations" --data "$BODY" | py "d['id']")
else
  log "updating destination $DEST"
  sc -X PUT "$ROUTERS/$ROUTER/destinations/$DEST" --data "$BODY" >/dev/null
fi
log "destination id: $DEST"

# ---- 5. telemetry link (one per project and region) -----------------------------------------------
log "creating/updating telemetry link"
sc -X PUT "$LINK" --data "{\"accessToken\":\"$TOKEN\",\"telemetryRouterId\":\"$ROUTER\",\"enabled\":true,\"displayName\":\"$NAME\",\"description\":\"audit logs of project $P\"}" | find_key status >&2 || true
for _ in $(seq 1 30); do
  STATE=$(sc "$LINK" | find_key status)
  log "link state: ${STATE:-?}"
  case "${STATE,,}" in active|ready|enabled|running) break;; esac
  sleep 10
done

# ---- 6. verification: trigger an audit event and query Loki ---------------------------------------
log "triggering an audit event (create + delete an observability credential)"
TMP=$("$STACKIT" observability credentials create --instance-id "$OBS" --project-id "$P" -y -o json)
TMP_USER=$(printf '%s' "$TMP" | find_key username)
[ -z "$TMP_USER" ] || "$STACKIT" observability credentials delete "$TMP_USER" --instance-id "$OBS" --project-id "$P" -y >/dev/null 2>&1 || true
LOKI=${OTLP%/otlp/v1/logs}/loki/api/v1
log "waiting for logs in Loki ($LOKI)"
for _ in $(seq 1 18); do
  sleep 10
  N=$(curl -s -u "$OBS_USER:$OBS_PASS" -G "$LOKI/query_range" --data-urlencode 'query={service_name=~".+"}' --data-urlencode "start=$(( $(date +%s) - 3600 ))000000000" --data-urlencode 'limit=5' | py "sum(len(s.get('values',[])) for s in d.get('data',{}).get('result',[]))" 2>/dev/null || echo 0)
  if [ "${N:-0}" -gt 0 ]; then log "OK: $N log line(s) found in Loki"; exit 0; fi
done
log "no log lines seen in Loki yet; check Grafana > Explore (Loki) with {service_name=~\".+\"} in a few minutes"
