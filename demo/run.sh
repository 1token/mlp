#!/bin/sh
# Two real domains on localhost — the S4.13 demo runtime.
# origin.demo :8441 (petra) and target.demo :8442 (novak), each a
# full mlpd: federation server, Body Store, Client API, web client.
#
#   ./demo/run.sh          # from the repo root
#
# Then open http://127.0.0.1:8441 (petra) and http://127.0.0.1:8442
# (novak) and follow demo/DEMO.md. Ctrl-C stops both.
set -eu
cd "$(dirname "$0")/.."
( cd server && go build -o ../demo/mlpd ./cmd/mlpd )
mkdir -p demo/data
./demo/mlpd -domain origin.demo -listen 127.0.0.1:8441 \
  -data demo/data/origin -client client \
  -peer origin.demo=http://127.0.0.1:8441 \
  -peer target.demo=http://127.0.0.1:8442 \
  -init petra -password "correct horse" &
ORIGIN=$!
./demo/mlpd -domain target.demo -listen 127.0.0.1:8442 \
  -data demo/data/target -client client \
  -peer origin.demo=http://127.0.0.1:8441 \
  -peer target.demo=http://127.0.0.1:8442 \
  -init novak -password "correct horse" &
TARGET=$!
trap 'kill $ORIGIN $TARGET 2>/dev/null' INT TERM
echo
echo "  petra  → http://127.0.0.1:8441   (origin.demo)"
echo "  novak  → http://127.0.0.1:8442   (target.demo)"
echo
wait
