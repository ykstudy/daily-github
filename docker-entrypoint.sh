#!/bin/sh
set -eu

data_dir="${DATA_DIR:-/app/data}"

mkdir -p "$data_dir"
chown -R app:app "$data_dir"

exec su-exec app /app/daily-github