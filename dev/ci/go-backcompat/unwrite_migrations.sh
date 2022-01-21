#!/usr/bin/env bash

# This script rewrites flat SQL migration files inot nested directories, as now
# organized by the main branch. This is run during backcompat tests while we still
# have to be backwards-compatible with versions prior to this directory structure
# change.

cd "$(dirname "${BASH_SOURCE[0]}")/../../../"
set -eu

function derange_bucket() {
  for dir in "${1}"/*; do
    version=$(basename "${dir}")
    up="${1}/${version}/up.sql"
    down="${1}/${version}/down.sql"
    metadata="${1}/${version}/metadata.yaml"
    name=$(head -n1 <"${metadata}" | cut -d' ' -f2- | tr ' ' '_' | tr -d \')
    up_target="${1}/${version}_${name}.up.sql"
    down_target="${1}/${version}_${name}.down.sql"
    sed -i -e 's/^/-- /' "${metadata}"

    {
      echo "-- +++"
      cat "${metadata}"
      echo "-- +++"
      cat "${up}"
    } >"${up_target}"

    mv "${down}" "${down_target}"
    rm -rf "${1:?}/${version}"
  done
}

function derange_migrations() {
  if ls "${1}"/*.sql; then
    echo "${1}: already up-to date"
  else
    echo "Deranging migrations for ${1}"
    derange_bucket "${1}"
  fi
}

prefixes=(
  './migrations/frontend'
  './migrations/codeintel'
  './migrations/codeinsights'
)

for prefix in "${prefixes[@]}"; do
  derange_migrations "${prefix}"
done
