#!/bin/bash

./dev/ci/go-backcompat/rewrite_migrations.sh

testdata_names=(
  'definition'
  'runner'
)

for name in "${testdata_names[@]}"; do
  prefix="internal/database/migration/${name}/testdata"
  cp "${prefix}"/embed.go ./temp.go
  rm -rf "${prefix:?}"/*
  tar -xvf "./experiment/${name}.tgz"
  cp ./temp.go "${prefix}"/embed.go
  rm -f temp.go
done
