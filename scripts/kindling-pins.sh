#!/bin/sh
# Pinned Kindling v0.45.6 source and official release assets. Keep hashes in
# sync with third_party/kindling/README.md and verify upstream before changes.
kindling_asset() {
  case "$1/$2" in
    darwin/arm64) printf '%s\n' 'kindling-cli-mac-apple-silicon' ;;
    darwin/amd64) printf '%s\n' 'kindling-cli-mac-intel' ;;
    linux/amd64) printf '%s\n' 'kindling-cli-linux' ;;
    linux/arm64) printf '%s\n' 'source-v0.45.6' ;;
    windows/amd64) printf '%s\n' 'kindling-cli-windows.exe' ;;
    *) echo "unsupported Kindling platform: $1/$2" >&2; return 1 ;;
  esac
}

kindling_sha256() {
  case "$1/$2" in
    darwin/arm64) printf '%s\n' '3de02c4ed420e3ebb42f2a88a8c911dbcf179e41fbedaa77e901dddef8d362a8' ;;
    darwin/amd64) printf '%s\n' 'e4ebea49f37907c35ae3d7678a61840333bb01779705424219c731c230a844dc' ;;
    linux/amd64) printf '%s\n' '417ad6dad4b1072fabb20610f320ba44ca8f4afcbe2f47948dcefc3955bd6439' ;;
    windows/amd64) printf '%s\n' '1988a319d0d9af2e75cf776eb9e63a59412fcd8ebfc5d0cebb000519ccd99f54' ;;
    *) echo "unsupported Kindling platform: $1/$2" >&2; return 1 ;;
  esac
}
