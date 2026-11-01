#!/usr/bin/env bash
# Creates a batch of workshop accounts end to end:
#   1. STACKIT account registration (user-add, register, verify-email, setup-otp), N at a time
#   2. meshStack trial workspace + STACKIT project + service account
#   3. terraform landing zone per project, zipped for download
#
# Usage: scripts/create-accounts.sh [prefix] [count]      (defaults: customername 20; an empty prefix "" gives slugs 1..N)
#        ACC_JOBS=5  limits the parallel STACKIT registrations (default 5; the IDP rate-limits sign-ups, 429s are retried)
#        TF_JOBS=3   limits the parallel terraform applies (default 3)
#        MESH_JOBS=3 limits the parallel meshStack sign-ups (default 3; with ~18 at once meshStack's
#                    trial setup answers "Something went wrong" and leaves workspaces without a project)
set -uo pipefail
cd "$(dirname "$0")/.."

PREFIX=${1-customername}
COUNT=${2:-20}
ACC_JOBS=${ACC_JOBS:-5}
MESH_JOBS=${MESH_JOBS:-3}
TF_JOBS=${TF_JOBS:-3}
BIN=${BIN:-./stackit-ws-manager}

mapfile -t SLUGS < <(seq -f "${PREFIX}%g" 1 "$COUNT")

echo "== STACKIT accounts (${ACC_JOBS} at a time): ${SLUGS[*]}"
parallel -j "$ACC_JOBS" --line-buffer --tag "$BIN stackit-acc onboard --create {}" ::: "${SLUGS[@]}"
acc_rc=$?

echo "== meshStack projects + service accounts (${MESH_JOBS} at a time)"
parallel -j "$MESH_JOBS" --line-buffer --tag "$BIN meshstack onboard {}" ::: "${SLUGS[@]}"
mesh_rc=$?

echo "== terraform landing zones (${TF_JOBS} at a time)"
parallel -j "$TF_JOBS" --line-buffer --tag "$BIN terraform deploy {}" ::: "${SLUGS[@]}"
tf_rc=$?

echo "== result"
"$BIN" stackit-acc user-list
[ "$acc_rc" -eq 0 ] && [ "$mesh_rc" -eq 0 ] && [ "$tf_rc" -eq 0 ] || { echo "some steps failed (accounts: $acc_rc failed, meshstack: $mesh_rc failed, terraform: $tf_rc failed) - rerun the script, finished steps are skipped"; exit 1; }
