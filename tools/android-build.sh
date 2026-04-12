#!/usr/bin/env bash

set -Eeuo pipefail

qt_android_copy=""
qt_android_source=""
qt_android_deploy_path="./.qt-android-runtime"
android_project_dir="./android-project"
selected_android_abis=()

get_app_name() {
  basename "$(pwd)"
}

get_stub_soname() {
  local abi="$1"
  printf 'lib%s_%s.so\n' "$(get_app_name)" "${abi}"
}

get_go_soname() {
  local abi="$1"
  printf 'libMiqtGolangApp_%s.so\n' "${abi}"
}

goarch_for_abi() {
  case "$1" in
    arm64-v8a)
      printf '%s\n' 'arm64'
      ;;
    x86_64)
      printf '%s\n' 'amd64'
      ;;
    *)
      die "Unsupported Android ABI: $1"
      ;;
  esac
}

cc_for_abi() {
  case "$1" in
    arm64-v8a)
      printf '%s\n' "${ANDROID_ARM64_CC}"
      ;;
    x86_64)
      printf '%s\n' "${ANDROID_X86_64_CC}"
      ;;
    *)
      die "Unsupported Android ABI: $1"
      ;;
  esac
}

cxx_for_abi() {
  case "$1" in
    arm64-v8a)
      printf '%s\n' "${ANDROID_ARM64_CXX}"
      ;;
    x86_64)
      printf '%s\n' "${ANDROID_X86_64_CXX}"
      ;;
    *)
      die "Unsupported Android ABI: $1"
      ;;
  esac
}

ar_for_abi() {
  case "$1" in
    arm64-v8a)
      printf '%s\n' "${ANDROID_ARM64_AR}"
      ;;
    x86_64)
      printf '%s\n' "${ANDROID_X86_64_AR}"
      ;;
    *)
      die "Unsupported Android ABI: $1"
      ;;
  esac
}

ndk_triple_for_abi() {
  case "$1" in
    arm64-v8a)
      printf '%s\n' 'aarch64-linux-android'
      ;;
    x86_64)
      printf '%s\n' 'x86_64-linux-android'
      ;;
    *)
      die "Unsupported Android ABI: $1"
      ;;
  esac
}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

require_is_main_package() {
  if ! grep -Fq 'package main' -- ./*.go; then
    die "This doesn't seem to be the main package"
  fi
}

load_selected_abis() {
  local abi_list abi
  local -a parsed_abis=()
  local -A seen_abis=()

  abi_list="${VOXORA_ANDROID_ABIS:-arm64-v8a,x86_64}"
  IFS=',' read -r -a parsed_abis <<< "${abi_list}"

  selected_android_abis=()
  for abi in "${parsed_abis[@]}"; do
    abi="${abi//[[:space:]]/}"
    [ -n "${abi}" ] || continue

    case "${abi}" in
      arm64-v8a|x86_64)
        ;;
      *)
        die "Unsupported Android ABI: ${abi}"
        ;;
    esac

    if [ -z "${seen_abis["${abi}"]:-}" ]; then
      selected_android_abis+=("${abi}")
      seen_abis["${abi}"]=1
    fi
  done

  if [ "${#selected_android_abis[@]}" -eq 0 ]; then
    die "No Android ABIs selected. Set VOXORA_ANDROID_ABIS to one or more of: arm64-v8a, x86_64"
  fi
}

load_signing_env() {
  if [ -f ./android.keystore.env ]; then
    set -a
    # shellcheck disable=SC1091
    . ./android.keystore.env
    set +a
  fi

  : "${QT_ANDROID_KEYSTORE_PATH:=./android.keystore}"
  : "${QT_ANDROID_KEYSTORE_ALIAS:?Set QT_ANDROID_KEYSTORE_ALIAS or provide android.keystore.env (or run nix run .#android-keystore-setup)}"
  : "${QT_ANDROID_KEYSTORE_STORE_PASS:?Set QT_ANDROID_KEYSTORE_STORE_PASS or provide android.keystore.env (or run nix run .#android-keystore-setup)}"
  : "${QT_ANDROID_KEYSTORE_KEY_PASS:?Set QT_ANDROID_KEYSTORE_KEY_PASS or provide android.keystore.env (or run nix run .#android-keystore-setup)}"

  if [ ! -f "${QT_ANDROID_KEYSTORE_PATH}" ]; then
    die "Missing keystore at ${QT_ANDROID_KEYSTORE_PATH}. Add voxora/android.keystore and signing env vars, provide android.keystore.env, or run nix run .#android-keystore-setup."
  fi
}

prepare_writable_qt_android() {
  local marker_file marker_value

  qt_android_source="${QT_ANDROID}"
  qt_android_copy="${qt_android_deploy_path}"
  marker_file="${qt_android_copy}/.nix-source-path"
  marker_value="${qt_android_source}|copy-v3"

  if [ ! -f "${marker_file}" ] || [ "$(<"${marker_file}")" != "${marker_value}" ]; then
    rm -rf "${qt_android_copy}"
    mkdir -p "${qt_android_copy}"
    cp -LR "${qt_android_source}/." "${qt_android_copy}/"
    chmod -R u+w "${qt_android_copy}"
    printf '%s\n' "${marker_value}" > "${marker_file}"
  fi

  export QT_ANDROID="$(pwd)/${qt_android_deploy_path#./}"
}

prepare_android_project() {
  mkdir -p "${android_project_dir}/res/values"
  rm -rf "${android_project_dir}/libs"
  mkdir -p "${android_project_dir}/libs"
  mkdir -p "${android_project_dir}/assets"
  rm -rf "${android_project_dir}/assets/qml" "${android_project_dir}/assets/android_rcc_bundle" "${android_project_dir}/assets/android_rcc_bundle.rcc" "${android_project_dir}/assets/android_rcc_bundle.qrc"

  cat > "${android_project_dir}/local.properties" <<EOF
sdk.dir=${ANDROID_SDK_ROOT}
EOF
}

generate_stub_source() {
  local stub_source="$1"
  local abi="$2"

  cat > "${stub_source}" <<EOF
#include <android/log.h>
#include <dlfcn.h>
#include <stdlib.h>

typedef void goMainFunc_t();

int main(int argc, char** argv) {
    __android_log_print(ANDROID_LOG_VERBOSE, "miqt_stub", "Starting up");

    void* handle = dlopen("$(get_go_soname "${abi}")", RTLD_LAZY);
    if (handle == NULL) {
        __android_log_print(ANDROID_LOG_VERBOSE, "miqt_stub", "miqt_stub: null handle opening so: %s", dlerror());
        exit(1);
    }

    void* goMain = dlsym(handle, "AndroidMain");
    if (goMain == NULL) {
        __android_log_print(ANDROID_LOG_VERBOSE, "miqt_stub", "miqt_stub: null handle looking for function: %s", dlerror());
        exit(1);
    }

    __android_log_print(ANDROID_LOG_VERBOSE, "miqt_stub", "miqt_stub: Found target, calling");

    goMainFunc_t* f = (goMainFunc_t*)goMain;
    f();

    __android_log_print(ANDROID_LOG_VERBOSE, "miqt_stub", "miqt_stub: Target function returned");
    return 0;
}
EOF
}

build_stub_library() {
  local abi="$1"
  local stub_source cxx

  stub_source="$(mktemp ./miqt-stub-XXXXXX.cpp)"
  cxx="$(cxx_for_abi "${abi}")"

  generate_stub_source "${stub_source}" "${abi}"

  "${cxx}" \
    -shared \
    -ldl \
    -llog \
    -L"${QT_ANDROID}/plugins/platforms" \
    -L"${QT_ANDROID}/lib" \
    -Wl,-rpath-link,"${QT_ANDROID}/lib" \
    -Wl,-z,max-page-size=16384 \
    -Wl,-soname,"$(get_stub_soname "${abi}")" \
    -o "${android_project_dir}/libs/${abi}/$(get_stub_soname "${abi}")" \
    "${stub_source}" \
    -lplugins_platforms_qtforandroid_${abi} \
    -l:libQt6Widgets_${abi}.so \
    -l:libQt6Gui_${abi}.so \
    -l:libQt6Core_${abi}.so

  rm -f "${stub_source}"
}

build_go_library() {
  local abi="$1"
  local extldflags goarch cc cxx ar

  goarch="$(goarch_for_abi "${abi}")"
  cc="$(cc_for_abi "${abi}")"
  cxx="$(cxx_for_abi "${abi}")"
  ar="$(ar_for_abi "${abi}")"
  extldflags="-Wl,-soname,$(get_go_soname "${abi}") -Wl,-z,max-page-size=16384"

  env \
    GOOS=android \
    GOARCH="${goarch}" \
    CC="${cc}" \
    CXX="${cxx}" \
    AR="${ar}" \
    PKG_CONFIG="${QT_ANDROID_PKG_CONFIG}" \
    QT_ANDROID_ABI="${abi}" \
    CGO_LDFLAGS="-Wl,-rpath-link,${QT_ANDROID}/lib -Wl,-z,max-page-size=16384" \
    go build \
    -buildmode c-shared \
    -ldflags "-s -w -extldflags '${extldflags}'" \
    -o "${android_project_dir}/libs/${abi}/$(get_go_soname "${abi}")"
}

seed_qt_runtime_for_abi() {
  local abi="$1"
  local triple

  cp -f "${QT_ANDROID}/jar/"*.jar "${android_project_dir}/libs/"

  triple="$(ndk_triple_for_abi "${abi}")"
  cp -f "${ANDROID_NDK_ROOT}/toolchains/llvm/prebuilt/${ANDROID_NDK_HOST}/sysroot/usr/lib/${triple}/libc++_shared.so" "${android_project_dir}/libs/${abi}/"

  while IFS= read -r -d '' so_file; do
    cp -f "${so_file}" "${android_project_dir}/libs/${abi}/"
  done < <(find "${QT_ANDROID}/lib" "${QT_ANDROID}/plugins" "${QT_ANDROID}/qml" -type f -name "*_${abi}.so" -print0)
}

bundle_qml_modules() {
  local bundle_dir scanner_output relative_path module_path

  bundle_dir="${android_project_dir}/assets/qml"
  scanner_output="$(mktemp ./qmlimportscanner.XXXXXX.json)"
  mkdir -p "${bundle_dir}"

  "${QT_QML_IMPORTSCANNER}" \
    -rootPath "$(pwd)" \
    -importPath "$(pwd)" \
    -importPath "${QT_ANDROID}/qml" \
    > "${scanner_output}"

  while IFS=$'\t' read -r relative_path module_path; do
    [ -n "${relative_path}" ] || continue
    [ -n "${module_path}" ] || continue

    mkdir -p "${bundle_dir}/${relative_path}"
    cp -LR "${module_path}/." "${bundle_dir}/${relative_path}/"
  done < <(jq -r '.[] | select(.type == "module" and (.path != null) and (.relativePath != null)) | [.relativePath, .path] | @tsv' "${scanner_output}" | sort -u)
  rm -f "${scanner_output}"
}

generate_libs_xml() {
  local abi libs_dir qt_name so_name
  local qt_items=""
  local load_local_items=""

  for abi in "${selected_android_abis[@]}"; do
    libs_dir="${android_project_dir}/libs/${abi}"

    qt_items+=$'\n'
    qt_items+="        <item>${abi};c++_shared</item>"

    shopt -s nullglob
    for so_name in "${libs_dir}"/libQt*.so; do
      qt_name="${so_name##*/}"
      qt_name="${qt_name#lib}"
      qt_name="${qt_name%.so}"
      qt_items+=$'\n'
      qt_items+="        <item>${abi};${qt_name}</item>"
    done
    shopt -u nullglob

    load_local_items+=$'\n'
    load_local_items+="        <item>${abi};libplugins_platforms_qtforandroid_${abi}.so:$(get_go_soname "${abi}")</item>"
  done

  cat > "${android_project_dir}/res/values/libs.xml" <<EOF
<?xml version='1.0' encoding='utf-8'?>
<resources>
    <array name="bundled_libs">
    </array>

    <array name="qt_libs">
${qt_items}
    </array>

    <array name="load_local_libs">
${load_local_items}
    </array>

    <string name="use_local_qt_libs">1</string>
    <string name="bundle_local_qt_libs">1</string>
    <string name="system_libs_prefix"></string>
</resources>
EOF
}

build_apk() {
  local abi_list out_apk unsigned_apk release_dir
  local -a unsigned_candidates

  abi_list="$(IFS=,; printf '%s' "${selected_android_abis[*]}")"
  generate_libs_xml

  (
    cd "${android_project_dir}"
    export GRADLE_USER_HOME="$(pwd)/../.gradle-android"
    export ORG_GRADLE_PROJECT_qtTargetAbiList="${abi_list}"
    gradle --no-daemon assembleRelease
  )

  release_dir="${android_project_dir}/build/outputs/apk/release"
  unsigned_candidates=("${release_dir}"/*-release-unsigned.apk)
  if [ "${#unsigned_candidates[@]}" -ne 1 ]; then
    die "Expected exactly one unsigned release APK in ${release_dir}"
  fi
  unsigned_apk="${unsigned_candidates[0]}"

  out_apk="$(get_app_name).apk"
  rm -f "${out_apk}"

  "${ANDROID_SDK_ROOT}/build-tools/${ANDROID_SDK_BUILD_TOOLS_REVISION}/zipalign" \
    -P 16 \
    -f \
    4 \
    "${unsigned_apk}" \
    "${out_apk}"

  "${ANDROID_SDK_ROOT}/build-tools/${ANDROID_SDK_BUILD_TOOLS_REVISION}/apksigner" \
    sign \
    --ks "${QT_ANDROID_KEYSTORE_PATH}" \
    --ks-key-alias "${QT_ANDROID_KEYSTORE_ALIAS}" \
    --ks-pass env:QT_ANDROID_KEYSTORE_STORE_PASS \
    --key-pass env:QT_ANDROID_KEYSTORE_KEY_PASS \
    "${out_apk}"
}

main() {
  local abi

  require_is_main_package
  load_signing_env
  load_selected_abis
  prepare_writable_qt_android
  prepare_android_project

  for abi in "${selected_android_abis[@]}"; do
    mkdir -p "${android_project_dir}/libs/${abi}"
    build_stub_library "${abi}"
    build_go_library "${abi}"
    seed_qt_runtime_for_abi "${abi}"
  done

  bundle_qml_modules

  build_apk
  printf '%s\n' 'Android APK build complete'
}

main "$@"
