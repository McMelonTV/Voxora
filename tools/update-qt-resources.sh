#!/usr/bin/env bash

set -Eeuo pipefail

project_dir="${1:-.}"

cd "${project_dir}"

assets_dir="./assets"

if [ ! -d "${assets_dir}" ]; then
  printf '%s\n' 'Expected an assets/ directory with app-owned bundled files.' >&2
  exit 1
fi

mapfile -t asset_files < <(find "${assets_dir}" -type f | sort)

if [ "${#asset_files[@]}" -eq 0 ]; then
  printf '%s\n' 'No app asset files found to package.' >&2
  exit 1
fi

{
  printf '%s\n' '<RCC>'
  printf '%s\n' '    <qresource prefix="/">'
  for asset_file in "${asset_files[@]}"; do
    printf '        <file>%s</file>\n' "${asset_file#./}"
  done
  printf '%s\n' '    </qresource>'
  printf '%s\n' '</RCC>'
} > resources.qrc

miqt-rcc -Qt6 -Input resources.qrc -OutputGo resources_qrc.go -Package main
gofmt -w resources_qrc.go
