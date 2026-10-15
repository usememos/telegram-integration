#!/usr/bin/env bash
set -euo pipefail

# Release tags are YY.MM.N[-rc.K], without a v prefix. N starts at 0, K at 1.
parse_release_tag() {
  local tag="$1"
  if [[ ! "$tag" =~ ^([1-9][0-9]\.(0[1-9]|1[0-2]))\.(0|[1-9][0-9]*)(-rc\.([1-9][0-9]*))?$ ]]; then
    return 1
  fi
  RELEASE_VERSION="$tag"
  RELEASE_SERIES="${BASH_REMATCH[1]}"
  RELEASE_IS_PRERELEASE=false
  if [[ -n "${BASH_REMATCH[5]}" ]]; then
    RELEASE_IS_PRERELEASE=true
  fi
}

development_version() {
  local head month day sha
  head="$(TZ=UTC git -C "$1" show -s --format='%cd %H' --date='format-local:%y.%m %d' HEAD)"
  read -r month day sha <<< "$head"
  parse_release_tag "$month.${day#0}"
  printf '%s-dev.%s\n' "$RELEASE_VERSION" "${sha:0:7}"
}

# Compare CalVer releases before the legacy v0.x history, and skip RCs and tags
# outside the release commit's ancestry. This also handles the first CalVer release.
previous_release_tag() {
  local repo="$1" head_sha tag tag_sha
  head_sha="$(git -C "$repo" rev-parse HEAD)"
  while IFS= read -r tag; do
    if parse_release_tag "$tag"; then
      [[ "$RELEASE_IS_PRERELEASE" == false ]] || continue
    elif [[ ! "$tag" =~ ^v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
      continue
    fi
    tag_sha="$(git -C "$repo" rev-list -n 1 "$tag")"
    [[ "$tag_sha" != "$head_sha" ]] || continue
    if git -C "$repo" merge-base --is-ancestor "$tag" HEAD; then
      printf '%s\n' "$tag"
      return
    fi
  done < <(
    git -C "$repo" tag --list '[1-9][0-9].*' --sort=-version:refname
    git -C "$repo" tag --list 'v0.*' --sort=-version:refname
  )
  echo 'No previous stable release found' >&2
  return 1
}

case "${1:-}" in
  development-version) development_version "${2:-.}" ;;
  previous-tag) previous_release_tag "${2:-.}" ;;
  version)
    if ! parse_release_tag "${2:-}"; then
      echo "Unsupported release tag format: ${2:-}" >&2
      exit 1
    fi
    printf 'tag=%s\nversion=%s\nseries=%s\nis_prerelease=%s\n' \
      "$RELEASE_VERSION" "$RELEASE_VERSION" "$RELEASE_SERIES" "$RELEASE_IS_PRERELEASE"
    ;;
  *) echo "Usage: $0 {version TAG|development-version [REPO]|previous-tag [REPO]}" >&2; exit 1 ;;
esac
