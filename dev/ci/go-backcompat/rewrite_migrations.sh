#!/usr/bin/env bash

# This script rewrites flat SQL migration files inot nested directories, as now
# organized by the main branch. This is run during backcompat tests while we still
# have to be backwards-compatible with versions prior to this directory structure
# change.

cd "$(dirname "${BASH_SOURCE[0]}")/../../../"
set -eu

function seed_bucket() {
  for file in "${1}"/*.sql; do
    dir="${1}/$(basename "${file}" | cut -d'_' -f1)"
    mkdir -p "${dir}"
    mv "${file}" "${dir}"
  done
}

function arrange_bucket() {
  local first=true
  for dir in "${1}"/*; do
    version=$(basename "${dir}")
    name=$(basename "${1}"/"${version}"/*.up.sql .up.sql | cut -d'_' -f2- | tr '_' ' ')
    echo "name: '${name}'" >"${1}/${version}/metadata.yaml"
    if ! $first; then
      echo "parent: $((version - 1))" >>"${1}/${version}/metadata.yaml"
    fi

    mv "${1}"/"${version}"/*.up.sql "${1}/${version}/up.sql"
    mv "${1}"/"${version}"/*.down.sql "${1}/${version}/down.sql"
    # TODO - remove `-- +++` metadata from up files?
    first=false
  done
}

function arrange_migrations() {
  pwd
  if ls "${1}"/*.sql; then
    echo "Rewriting migrations for ${1}"
    seed_bucket "${1}"
    arrange_bucket "${1}"
  else
    echo "${1}: already up-to date"
  fi
}

prefixes=(
  './migrations/frontend'
  './migrations/codeintel'
  './migrations/codeinsights'
)

for prefix in "${prefixes[@]}"; do
  arrange_migrations "${prefix}"
done
