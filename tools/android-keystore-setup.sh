#!/usr/bin/env bash

set -Eeuo pipefail

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: android-keystore-setup.sh [options]

Creates voxora/android.keystore and voxora/android.keystore.env for APK signing.

Options:
  --alias <name>           Keystore alias (default: voxora-release)
  --keystore <path>        Keystore output path (default: <repo>/voxora/android.keystore)
  --env-file <path>        Signing env output path (default: <repo>/voxora/android.keystore.env)
  --validity-days <days>   Certificate validity (default: 10000)
  --dname <value>          Distinguished name for the cert
  --force                  Overwrite existing keystore/env files
  --help                   Show this message

Optional environment variables:
  QT_ANDROID_KEYSTORE_ALIAS
  QT_ANDROID_KEYSTORE_STORE_PASS
  QT_ANDROID_KEYSTORE_KEY_PASS
  QT_ANDROID_KEYSTORE_VALIDITY_DAYS
  QT_ANDROID_KEYSTORE_DNAME
EOF
}

random_secret() {
  local value
  value="$(head -c 48 /dev/urandom | base64 | tr -d '=+/' | cut -c1-48)"
  if [ -z "${value}" ]; then
    die "Failed to generate signing password"
  fi
  printf '%s\n' "${value}"
}

write_env_var() {
  local key="$1"
  local value="$2"
  printf '%s=%q\n' "${key}" "${value}" >> "${env_path}"
}

resolve_keystore_env_path() {
  if [ "${keystore_path}" = "${default_keystore}" ]; then
    printf '%s\n' "./android.keystore"
    return
  fi

  printf '%s\n' "${keystore_path}"
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/.." && pwd)"

default_keystore="${repo_root}/voxora/android.keystore"
default_env_file="${repo_root}/voxora/android.keystore.env"

keystore_path="${default_keystore}"
env_path="${default_env_file}"
alias_name="${QT_ANDROID_KEYSTORE_ALIAS:-voxora-release}"
validity_days="${QT_ANDROID_KEYSTORE_VALIDITY_DAYS:-10000}"
dname_value="${QT_ANDROID_KEYSTORE_DNAME:-CN=Voxora Release, OU=Mobile, O=Voxora, L=Unknown, ST=Unknown, C=US}"
force="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --alias)
      [ "$#" -ge 2 ] || die "Missing value for --alias"
      alias_name="$2"
      shift 2
      ;;
    --keystore)
      [ "$#" -ge 2 ] || die "Missing value for --keystore"
      keystore_path="$2"
      shift 2
      ;;
    --env-file)
      [ "$#" -ge 2 ] || die "Missing value for --env-file"
      env_path="$2"
      shift 2
      ;;
    --validity-days)
      [ "$#" -ge 2 ] || die "Missing value for --validity-days"
      validity_days="$2"
      shift 2
      ;;
    --dname)
      [ "$#" -ge 2 ] || die "Missing value for --dname"
      dname_value="$2"
      shift 2
      ;;
    --force)
      force="true"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "Unknown option: $1"
      ;;
  esac
done

if [ "${force}" != "true" ]; then
  [ ! -e "${keystore_path}" ] || die "Keystore already exists at ${keystore_path} (use --force to overwrite)"
  [ ! -e "${env_path}" ] || die "Env file already exists at ${env_path} (use --force to overwrite)"
fi

mkdir -p "$(dirname "${keystore_path}")"
mkdir -p "$(dirname "${env_path}")"

store_pass="${QT_ANDROID_KEYSTORE_STORE_PASS:-$(random_secret)}"
key_pass="${QT_ANDROID_KEYSTORE_KEY_PASS:-${store_pass}}"

rm -f "${keystore_path}" "${env_path}"

keytool -genkeypair \
  -storetype PKCS12 \
  -keystore "${keystore_path}" \
  -alias "${alias_name}" \
  -keyalg RSA \
  -keysize 4096 \
  -validity "${validity_days}" \
  -dname "${dname_value}" \
  -storepass "${store_pass}" \
  -keypass "${key_pass}" \
  -noprompt

touch "${env_path}"
chmod 600 "${env_path}"

write_env_var QT_ANDROID_KEYSTORE_PATH "$(resolve_keystore_env_path)"
write_env_var QT_ANDROID_KEYSTORE_ALIAS "${alias_name}"
write_env_var QT_ANDROID_KEYSTORE_STORE_PASS "${store_pass}"
write_env_var QT_ANDROID_KEYSTORE_KEY_PASS "${key_pass}"

printf '%s\n' "Keystore created at ${keystore_path}"
printf '%s\n' "Signing env written to ${env_path}"
printf '%s\n' "Run nix run .#android-build to produce a signed APK."
