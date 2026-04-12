#!/usr/bin/env bash

set -euo pipefail

qt_root="${QT_ANDROID:?QT_ANDROID must point at the Qt for Android runtime root}"
qt_abi="${QT_ANDROID_ABI:-arm64-v8a}"

want_cflags=false
want_libs=false
declare -a packages=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --cflags)
      want_cflags=true
      ;;
    --libs)
      want_libs=true
      ;;
    --)
      ;;
    --static|--silence-errors)
      ;;
    --*)
      printf 'Unsupported pkg-config flag: %s\n' "$1" >&2
      exit 1
      ;;
    *)
      packages+=("$1")
      ;;
  esac
  shift
done

if [ "${#packages[@]}" -eq 0 ]; then
  exit 0
fi

declare -A seen_packages=()
declare -A seen_flags=()
declare -a cflags=()
declare -a libs=()

push_flag() {
  local flag="$1"
  local target="$2"

  if [ -n "${seen_flags["${flag}"]:-}" ]; then
    return
  fi

  seen_flags["${flag}"]=1
  if [ "${target}" = cflags ]; then
    cflags+=("${flag}")
  else
    libs+=("${flag}")
  fi
}

add_common_includes() {
  push_flag "-I${qt_root}/include" cflags
  push_flag "-I${qt_root}/mkspecs/android-clang" cflags
}

add_package() {
  local pkg="$1"

  if [ -n "${seen_packages["${pkg}"]:-}" ]; then
    return
  fi
  seen_packages["${pkg}"]=1

  case "${pkg}" in
    Qt6Core)
      add_common_includes
      push_flag "-I${qt_root}/include/QtCore" cflags
      push_flag "-DQT_CORE_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Core_${qt_abi}.so" libs
      ;;
    Qt6Gui)
      add_package Qt6Core
      push_flag "-I${qt_root}/include/QtGui" cflags
      push_flag "-DQT_GUI_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Gui_${qt_abi}.so" libs
      ;;
    Qt6Widgets)
      add_package Qt6Gui
      push_flag "-I${qt_root}/include/QtWidgets" cflags
      push_flag "-DQT_WIDGETS_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Widgets_${qt_abi}.so" libs
      ;;
    Qt6Network)
      add_package Qt6Core
      push_flag "-I${qt_root}/include/QtNetwork" cflags
      push_flag "-DQT_NETWORK_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Network_${qt_abi}.so" libs
      ;;
    Qt6Qml)
      add_package Qt6Network
      push_flag "-I${qt_root}/include/QtQml" cflags
      push_flag "-I${qt_root}/include/QtQmlIntegration" cflags
      push_flag "-DQT_QML_LIB" cflags
      push_flag "-DQT_QMLINTEGRATION_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Qml_${qt_abi}.so" libs
      push_flag "${qt_root}/lib/libQt6QmlModels_${qt_abi}.so" libs
      push_flag "${qt_root}/lib/libQt6QmlNetwork_${qt_abi}.so" libs
      ;;
    Qt6Svg)
      add_package Qt6Gui
      push_flag "-I${qt_root}/include/QtSvg" cflags
      push_flag "-DQT_SVG_LIB" cflags
      push_flag "${qt_root}/lib/libQt6Svg_${qt_abi}.so" libs
      ;;
    Qt6SvgWidgets)
      add_package Qt6Svg
      add_package Qt6Widgets
      ;;
    *)
      printf 'Unsupported Qt package for Android build: %s\n' "${pkg}" >&2
      exit 1
      ;;
  esac
}

for pkg in "${packages[@]}"; do
  add_package "${pkg}"
done

output=()
if [ "${want_cflags}" = true ]; then
  output+=("${cflags[@]}")
fi
if [ "${want_libs}" = true ]; then
  output+=("-Wl,-rpath-link,${qt_root}/lib")
  output+=("${libs[@]}")
fi

printf '%s\n' "${output[*]}"
