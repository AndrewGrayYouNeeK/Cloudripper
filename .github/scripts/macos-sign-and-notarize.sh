#!/usr/bin/env bash
# Sign and notarize the Wails macOS .app on GitHub Actions.
#
# Subcommands:
#   enabled  Record enabled=true/false in $GITHUB_OUTPUT. Pull requests stay
#            unsigned. Pushes to main and version tags require the signing
#            secrets and fail closed when any secret is missing.
#   sign     Import the Developer ID identity, sign the .app, notarize, staple,
#            and re-zip. Removes the keychain and key files on exit.
#   cleanup  Delete the temporary keychain and key files. Safe to run twice.
#
# No entitlements plist: Wails production apps use the system WKWebView, so
# JavaScript JIT stays in Apple's WebContent process. Go 1.26's darwin
# allocator does not request MAP_JIT for an ordinary binary, and this app is
# not sandboxed (it reads local cloud credentials). allow-jit is a development
# entitlement and is not required here. Network entitlements apply only when
# the App Sandbox is on.
#
# Nested Mach-O files are signed inside-out. codesign --deep is used only for
# verification; Apple's signing guidance rejects it as a way to sign a bundle.

set -euo pipefail

: "${RUNNER_TEMP:=${TMPDIR:-/tmp}}"

KEYCHAIN_PATH="${RUNNER_TEMP}/cloudripper-signing.keychain-db"
P12_PATH="${RUNNER_TEMP}/cloudripper-developer-id.p12"
P8_PATH="${RUNNER_TEMP}/AuthKey.p8"
NOTARIZE_ZIP="${RUNNER_TEMP}/cloudripper-notarize.zip"
NOTARY_JSON="${RUNNER_TEMP}/notary-submit.json"
NOTARY_ERR="${RUNNER_TEMP}/notary-submit.err"
KEYCHAIN_LIST_PATH="${RUNNER_TEMP}/cloudripper-keychain-list.txt"
DEFAULT_KEYCHAIN_PATH="${RUNNER_TEMP}/cloudripper-default-keychain.txt"

cmd_cleanup() {
  local login="${HOME}/Library/Keychains/login.keychain-db"
  local line
  local -a chains=()

  # Pull-request runs never create a keychain. Leave the runner keychains
  # alone unless this job actually imported a certificate or saved the
  # previous search list.
  if [ ! -f "$DEFAULT_KEYCHAIN_PATH" ] && [ ! -f "$KEYCHAIN_LIST_PATH" ] && [ ! -f "$KEYCHAIN_PATH" ] && [ ! -f "$P12_PATH" ] && [ ! -f "$P8_PATH" ]; then
    rm -f "$NOTARIZE_ZIP" "$NOTARY_JSON" "$NOTARY_ERR"
    return 0
  fi

  if [ -f "$DEFAULT_KEYCHAIN_PATH" ]; then
    line=$(sed -e 's/^[[:space:]]*"\{0,1\}//' -e 's/"\{0,1\}[[:space:]]*$//' "$DEFAULT_KEYCHAIN_PATH")
    if [ -n "$line" ] && [ -f "$line" ]; then
      security default-keychain -s "$line" || true
    fi
  elif [ -f "$login" ]; then
    security default-keychain -s "$login" || true
  fi

  if [ -f "$KEYCHAIN_LIST_PATH" ]; then
    while IFS= read -r line; do
      line=$(printf '%s' "$line" | sed -e 's/^[[:space:]]*"\{0,1\}//' -e 's/"\{0,1\}[[:space:]]*$//')
      if [ -n "$line" ]; then
        chains+=("$line")
      fi
    done < "$KEYCHAIN_LIST_PATH"
    if [ "${#chains[@]}" -gt 0 ]; then
      security list-keychains -d user -s "${chains[@]}" || true
    fi
  elif [ -f "$login" ]; then
    security list-keychains -d user -s "$login" || true
  fi

  if [ -f "$KEYCHAIN_PATH" ]; then
    security delete-keychain "$KEYCHAIN_PATH" || rm -f "$KEYCHAIN_PATH" || true
  fi

  rm -f \
    "$P12_PATH" \
    "$P8_PATH" \
    "$NOTARIZE_ZIP" \
    "$NOTARY_JSON" \
    "$NOTARY_ERR" \
    "$KEYCHAIN_LIST_PATH" \
    "$DEFAULT_KEYCHAIN_PATH"

  return 0
}

cmd_enabled() {
  local missing=0
  local name
  local val

  if [ -z "${GITHUB_OUTPUT:-}" ]; then
    echo "GITHUB_OUTPUT is required" >&2
    return 1
  fi

  if [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ]; then
    echo "Pull request build: skipping code signing and notarization."
    echo "enabled=false" >> "$GITHUB_OUTPUT"
    return 0
  fi

  case "${GITHUB_REF:-}" in
    refs/heads/main|refs/tags/v*)
      ;;
    *)
      echo "Ref ${GITHUB_REF:-unknown} is not a signing ref. Leaving the macOS build unsigned."
      echo "enabled=false" >> "$GITHUB_OUTPUT"
      return 0
      ;;
  esac

  for name in \
    MACOS_CERTIFICATE_P12_BASE64 \
    MACOS_CERTIFICATE_PASSWORD \
    MACOS_SIGNING_IDENTITY \
    APPLE_TEAM_ID \
    APPLE_API_KEY_P8_BASE64 \
    APPLE_API_KEY_ID \
    APPLE_API_ISSUER_ID
  do
    eval "val=\${$name-}"
    if [ -z "$val" ]; then
      echo "Signing secret ${name} is not set." >&2
      missing=1
    fi
  done

  if [ "$missing" -ne 0 ]; then
    echo "Refusing to upload an unsigned macOS build for ${GITHUB_REF}." >&2
    return 1
  fi

  echo "Signing secrets are present. The macOS app will be signed and notarized."
  echo "enabled=true" >> "$GITHUB_OUTPUT"
}

b64_decode() {
  local payload="$1"
  local dest="$2"
  if ! printf '%s' "$payload" | /usr/bin/base64 -D > "$dest"; then
    echo "Could not decode ${dest##*/}." >&2
    rm -f "$dest"
    return 1
  fi
  if [ ! -s "$dest" ]; then
    echo "Decoded ${dest##*/} is empty." >&2
    rm -f "$dest"
    return 1
  fi
  chmod 600 "$dest"
}

sign_one() {
  local target="$1"
  echo "Signing ${target}"
  codesign --force --options runtime --timestamp \
    --sign "$MACOS_SIGNING_IDENTITY" \
    --keychain "$KEYCHAIN_PATH" \
    "$target"
}

sign_nested() {
  local app="$1"
  local list
  local target
  local file_out

  list=$(mktemp)
  find "$app" -depth -type f ! -path '*/_CodeSignature/*' -print0 > "$list"
  while IFS= read -r -d '' target; do
    file_out=$(/usr/bin/file -b -- "$target" || true)
    if [[ "$file_out" == *Mach-O* ]]; then
      sign_one "$target" || return 1
    fi
  done < "$list"
  rm -f "$list"

  list=$(mktemp)
  find "$app" -depth -type d \( \
    -name '*.app' -o \
    -name '*.framework' -o \
    -name '*.xpc' -o \
    -name '*.appex' -o \
    -name '*.bundle' \
  \) -print0 > "$list"
  while IFS= read -r -d '' target; do
    sign_one "$target" || return 1
  done < "$list"
  rm -f "$list"
}

assess_gatekeeper() {
  local phase="$1"
  local app="$2"
  local out
  local rc

  out=$(mktemp)
  rc=0
  spctl -a -vv "$app" >"$out" 2>&1 || rc=$?
  cat "$out"
  if [ "$rc" -eq 0 ]; then
    rm -f "$out"
    return 0
  fi
  if [ "$phase" = "before-notarization" ] && /usr/bin/grep -q 'Unnotarized Developer ID' "$out"; then
    echo "Developer ID signature is present. Gatekeeper accepts the app after notarization."
    rm -f "$out"
    return 0
  fi
  rm -f "$out"
  echo "spctl rejected the app (${phase})." >&2
  return "$rc"
}

read_notary_field() {
  local json_path="$1"
  local field="$2"
  /usr/bin/python3 - "$json_path" "$field" <<'PY'
import json
import sys

path, field = sys.argv[1], sys.argv[2]
try:
    with open(path, "r", encoding="utf-8") as handle:
        raw = handle.read()
except OSError:
    sys.exit(0)

decoder = json.JSONDecoder()
found = []
index = 0
while True:
    start = raw.find("{", index)
    if start < 0:
        break
    try:
        obj, end = decoder.raw_decode(raw, start)
    except json.JSONDecodeError:
        index = start + 1
        continue
    if isinstance(obj, dict):
        found.append(obj)
    index = end if end > start else start + 1

data = found[-1] if found else {}
value = data.get(field) or ""
if not isinstance(value, str):
    value = str(value)
sys.stdout.write(value)
PY
}

print_notary_log() {
  local submission_id="$1"
  if [ -z "$submission_id" ]; then
    echo "Notarization failed before Apple returned a submission id." >&2
    return 0
  fi
  echo "Notarization log for ${submission_id}:"
  xcrun notarytool log "$submission_id" \
    --key "$P8_PATH" \
    --key-id "$APPLE_API_KEY_ID" \
    --issuer "$APPLE_API_ISSUER_ID" || true
}

cmd_sign() {
  local app="${1:-}"
  local submission_id=""
  local status=""
  local submit_rc=0
  local final_zip
  local attempt

  if [ -z "$app" ] || [ ! -d "$app/Contents/MacOS" ]; then
    echo "Expected a macOS .app bundle, got: ${app:-<empty>}" >&2
    return 1
  fi

  : "${MACOS_CERTIFICATE_P12_BASE64:?MACOS_CERTIFICATE_P12_BASE64 is required}"
  : "${MACOS_CERTIFICATE_PASSWORD:?MACOS_CERTIFICATE_PASSWORD is required}"
  : "${MACOS_SIGNING_IDENTITY:?MACOS_SIGNING_IDENTITY is required}"
  : "${APPLE_TEAM_ID:?APPLE_TEAM_ID is required}"
  : "${APPLE_API_KEY_P8_BASE64:?APPLE_API_KEY_P8_BASE64 is required}"
  : "${APPLE_API_KEY_ID:?APPLE_API_KEY_ID is required}"
  : "${APPLE_API_ISSUER_ID:?APPLE_API_ISSUER_ID is required}"

  case "$MACOS_SIGNING_IDENTITY" in
    *"$APPLE_TEAM_ID"*) ;;
    *)
      echo "The signing identity does not include the configured Apple team id." >&2
      return 1
      ;;
  esac

  umask 077
  security default-keychain > "$DEFAULT_KEYCHAIN_PATH"
  security list-keychains -d user > "$KEYCHAIN_LIST_PATH"

  b64_decode "$MACOS_CERTIFICATE_P12_BASE64" "$P12_PATH"
  unset MACOS_CERTIFICATE_P12_BASE64
  b64_decode "$APPLE_API_KEY_P8_BASE64" "$P8_PATH"
  unset APPLE_API_KEY_P8_BASE64

  local keychain_password
  keychain_password=$(openssl rand -base64 32)
  if [ -f "$KEYCHAIN_PATH" ]; then
    security delete-keychain "$KEYCHAIN_PATH" || rm -f "$KEYCHAIN_PATH"
  fi
  security create-keychain -p "$keychain_password" "$KEYCHAIN_PATH"
  security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
  security unlock-keychain -p "$keychain_password" "$KEYCHAIN_PATH"
  security import "$P12_PATH" \
    -k "$KEYCHAIN_PATH" \
    -P "$MACOS_CERTIFICATE_PASSWORD" \
    -f pkcs12 \
    -A \
    -T /usr/bin/codesign \
    -T /usr/bin/security
  security set-key-partition-list \
    -S apple-tool:,apple:,codesign: \
    -s \
    -k "$keychain_password" \
    "$KEYCHAIN_PATH"
  unset keychain_password MACOS_CERTIFICATE_PASSWORD
  rm -f "$P12_PATH"

  # Keep the previous search list, with the temporary keychain first.
  local -a previous=()
  local line
  while IFS= read -r line; do
    line=$(printf '%s' "$line" | sed -e 's/^[[:space:]]*"\{0,1\}//' -e 's/"\{0,1\}[[:space:]]*$//')
    if [ -n "$line" ]; then
      previous+=("$line")
    fi
  done < "$KEYCHAIN_LIST_PATH"
  security list-keychains -d user -s "$KEYCHAIN_PATH" ${previous[@]+"${previous[@]}"}
  security default-keychain -s "$KEYCHAIN_PATH"

  local identities
  identities=$(security find-identity -v -p codesigning "$KEYCHAIN_PATH" || true)
  if [[ "$identities" != *"$MACOS_SIGNING_IDENTITY"* ]]; then
    echo "Developer ID identity was not found in the temporary keychain." >&2
    printf '%s\n' "$identities" >&2
    return 1
  fi

  xattr -cr "$app"
  local signature_dirs
  local signature_dir
  signature_dirs=$(mktemp)
  find "$app" -depth -type d -name '_CodeSignature' -print0 > "$signature_dirs"
  while IFS= read -r -d '' signature_dir; do
    rm -rf "$signature_dir"
  done < "$signature_dirs"
  rm -f "$signature_dirs" "$app/Contents/CodeResources"

  sign_nested "$app"
  codesign --verify --deep --strict --verbose=2 "$app"
  codesign -dv --verbose=2 "$app"
  assess_gatekeeper before-notarization "$app"

  rm -f "$NOTARIZE_ZIP"
  ditto -c -k --keepParent "$app" "$NOTARIZE_ZIP"

  set +e
  xcrun notarytool submit "$NOTARIZE_ZIP" \
    --key "$P8_PATH" \
    --key-id "$APPLE_API_KEY_ID" \
    --issuer "$APPLE_API_ISSUER_ID" \
    --wait \
    --timeout 30m \
    --output-format json \
    >"$NOTARY_JSON" 2>"$NOTARY_ERR"
  submit_rc=$?
  set -e
  if [ -s "$NOTARY_ERR" ]; then
    cat "$NOTARY_ERR" >&2
  fi

  submission_id=$(read_notary_field "$NOTARY_JSON" id || true)
  status=$(read_notary_field "$NOTARY_JSON" status || true)
  echo "notarytool exit ${submit_rc}, status: ${status:-unknown}"

  if [ "$status" != "Accepted" ]; then
    echo "Notarization status is not Accepted." >&2
    if [ -s "$NOTARY_JSON" ]; then
      cat "$NOTARY_JSON" >&2
    fi
    print_notary_log "$submission_id"
    return 1
  fi

  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  codesign --verify --deep --strict --verbose=2 "$app"

  attempt=1
  while [ "$attempt" -le 3 ]; do
    if assess_gatekeeper after-notarization "$app"; then
      break
    fi
    if [ "$attempt" -eq 3 ]; then
      return 1
    fi
    echo "Retrying Gatekeeper assessment (${attempt})."
    attempt=$((attempt + 1))
    sleep 10
  done

  final_zip="$(dirname "$app")/cloudripper-desktop-macos.zip"
  rm -f "$final_zip"
  ditto -c -k --keepParent "$app" "$final_zip"
  echo "Stapled notarized app: ${final_zip}"
}

main() {
  local command="${1:-}"
  case "$command" in
    enabled)
      cmd_enabled
      ;;
    cleanup)
      cmd_cleanup
      ;;
    sign)
      shift
      trap 'status=$?; cmd_cleanup; exit "$status"' EXIT
      cmd_sign "$@"
      ;;
    *)
      echo "Usage: macos-sign-and-notarize.sh {enabled|sign|cleanup} [app]" >&2
      return 2
      ;;
  esac
}

main "$@"
