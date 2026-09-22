#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
printf 'Enygma presentation demo: http://127.0.0.1:4173\n'
printf 'Press Ctrl-C to stop.\n'
python3 -m http.server 4173 --bind 127.0.0.1
