#!/bin/bash

prefixes=(
  './migrations/frontend'
  './migrations/codeintel'
  './migrations/codeinsights'
)

for prefix in "${prefixes[@]}"; do
  rm -rf "${prefix:?}"/*
  git checkout main -- "${prefix}"
done

testdata_names=(
  'definition'
  'runner'
)

for name in "${testdata_names[@]}"; do
  prefix="./internal/database/migration/${name}/testdata"

  # back-up changes
  tar -cvf "./experiment/${name}.tgz" "${prefix}"

  cp "${prefix}"/embed.go ./temp.go
  rm -rf "${prefix:?}"/*
  git checkout main -- "${prefix}"
  cp ./temp.go "${prefix}"/embed.go
  rm -f temp.go
done
