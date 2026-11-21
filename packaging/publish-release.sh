#!/usr/bin/env bash
set -euo pipefail

assets=${1:?missing verified asset directory}
tag=${2:?missing release tag}
[[ "$tag" =~ ^v[0-9] ]]
commit=$(git rev-parse HEAD)
remote_commit=$(gh api "repos/{owner}/{repo}/commits/$tag" --jq .sha)
if [[ "$remote_commit" != "$commit" ]]; then
  echo "The remote tag $tag no longer points to the verified commit $commit." >&2
  exit 1
fi

# Work on a draft so users only see the release once every asset is available.
if ! release_url=$(gh release view "$tag" --json apiUrl --jq .apiUrl); then
  flags=(--draft --verify-tag --generate-notes --title "xlui $tag")
  if [[ "${tag%%+*}" == *-* ]]; then flags+=(--prerelease); fi
  gh release create "$tag" "${flags[@]}" --notes \
    'Linux downloads include standalone binaries, DEB and RPM packages for x86-64 and ARM64. Download SHA256SUMS alongside your chosen file to verify it.'
  release_url=$(gh release view "$tag" --json apiUrl --jq .apiUrl)
fi

release=$(gh api "$release_url")
if [[ $(jq -r .draft <<< "$release") == true ]]; then
  gh release upload "$tag" "$assets"/* --clobber
  release=$(gh api "$release_url")
fi

# A retry against a published release verifies it without replacing any files.
for file in "$assets"/*; do
  checksum=$(sha256sum < "$file")
  if ! jq -e --arg name "${file##*/}" --arg digest "sha256:${checksum%% *}" \
    'any(.assets[]; .name == $name and .state == "uploaded" and .digest == $digest)' \
    <<< "$release" > /dev/null; then
    echo "Release asset verification failed for ${file##*/}." >&2
    exit 1
  fi
done

if [[ $(jq -r .draft <<< "$release") == true ]]; then
  gh release edit "$tag" --draft=false
fi
