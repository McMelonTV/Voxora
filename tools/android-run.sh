#!/usr/bin/env bash

set -Eeuo pipefail

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

list_connected_devices() {
  local serial status extra

  while IFS=$'\t' read -r serial status extra; do
    [ -n "${serial}" ] || continue
    [ "${serial}" = "List of devices attached" ] && continue
    [ "${status}" = "device" ] || continue
    printf '%s\n' "${serial}"
  done < <("${ANDROID_ADB}" devices)
}

select_device() {
  local serial
  local -a devices=()

  if [ -n "${ANDROID_SERIAL:-}" ]; then
    printf '%s\n' "${ANDROID_SERIAL}"
    return 0
  fi

  mapfile -t devices < <(list_connected_devices)
  if [ "${#devices[@]}" -eq 0 ]; then
    die "No connected adb devices found. Connect a device or set ANDROID_SERIAL."
  fi

  if [ "${#devices[@]}" -gt 1 ]; then
    die "Multiple adb devices found. Set ANDROID_SERIAL to choose one."
  fi

  serial="${devices[0]}"
  printf '%s\n' "${serial}"
}

build_current_apk() {
  printf '%s\n' 'Building current APK...'
  (cd "${VOXORA_REPO_ROOT}" && "${VOXORA_ANDROID_BUILD_BIN}")
}

preferred_abi_for_device() {
  local serial abilist

  serial="$1"
  abilist="$("${ANDROID_ADB}" -s "${serial}" shell getprop ro.product.cpu.abilist | tr -d '\r')"

  case ",${abilist}," in
    *,x86_64,*)
      printf '%s\n' 'x86_64'
      ;;
    *,arm64-v8a,*)
      printf '%s\n' 'arm64-v8a'
      ;;
    *)
      die "Could not infer a supported ABI from device abilist: ${abilist}"
      ;;
  esac
}

safe_serial_name() {
  printf '%s' "$1" | tr -c 'A-Za-z0-9._-' '_'
}

should_install_apk() {
  local serial="$1"
  local apk_hash="$2"
  local state_file="$3"

  if ! "${ANDROID_ADB}" -s "${serial}" shell pm path "${VOXORA_ANDROID_PACKAGE_NAME}" >/dev/null 2>&1; then
    return 0
  fi

  if [ ! -f "${state_file}" ]; then
    return 0
  fi

  [ "$(<"${state_file}")" != "${apk_hash}" ]
}

main() {
  local serial abi apk_hash state_dir state_file

  serial="$(select_device)"
  abi="${VOXORA_ANDROID_ABIS:-$(preferred_abi_for_device "${serial}")}"
  export VOXORA_ANDROID_ABIS="${abi}"

  build_current_apk
  apk_hash="$(sha256sum ./voxora.apk | cut -d' ' -f1)"
  state_dir="./.android-run"
  mkdir -p "${state_dir}"
  state_file="${state_dir}/$(safe_serial_name "${serial}").sha256"

  if should_install_apk "${serial}" "${apk_hash}" "${state_file}"; then
    printf 'Installing APK on %s...\n' "${serial}"
    "${ANDROID_ADB}" -s "${serial}" install -r ./voxora.apk
    printf '%s\n' "${apk_hash}" > "${state_file}"
  else
    printf 'APK unchanged on %s, skipping reinstall.\n' "${serial}"
  fi

  printf 'Launching %s on %s...\n' "${VOXORA_ANDROID_PACKAGE_NAME}" "${serial}"
  exec "${ANDROID_ADB}" -s "${serial}" shell am start -n "${VOXORA_ANDROID_PACKAGE_NAME}/org.qtproject.qt.android.bindings.QtActivity"
}

main "$@"
