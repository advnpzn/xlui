#!/usr/bin/env bash
# Run from the repository root after goreleaser release. Stage only verified files.
set -euo pipefail
export LC_ALL=C
output=${1:?usage: bash packaging/verify-linux.sh OUTPUT_DIRECTORY}
mkdir "$output"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

artifact_path() {
  jq -er --arg arch "$1" --arg format "$2" '
    [.[] | select(.goarch == $arch and .extra.Format == $format) | .path]
    | if length == 1 then .[0] else error("expected one " + $arch + " " + $format) end
  ' dist/artifacts.json
}

for arch in amd64 arm64; do
  case "$arch" in
    amd64) rpm_arch=x86_64; machine='Advanced Micro Devices X86-64' ;;
    arm64) rpm_arch=aarch64; machine=AArch64 ;;
  esac
  binary=$(artifact_path "$arch" binary)
  deb=$(artifact_path "$arch" deb)
  rpm_file=$(artifact_path "$arch" rpm)
  header=$(readelf -h "$binary")
  [[ "$header" == *"$machine"* ]]
  segments=$(readelf -lW "$binary")
  dynamic=$(readelf -dW "$binary")
  [[ "$segments" != *INTERP* && "$dynamic" != *NEEDED* ]]

  [[ "$(dpkg-deb -f "$deb" Package)" == xlui ]]
  [[ "$(dpkg-deb -f "$deb" Architecture)" == "$arch" ]]
  [[ "$(rpm -qp --qf '%{NAME} %{ARCH}' "$rpm_file")" == "xlui $rpm_arch" ]]
  dpkg-deb -x "$deb" "$work/deb-$arch"
  cmp "$binary" "$work/deb-$arch/usr/bin/xlui"
  rpm2cpio "$rpm_file" | cpio --quiet -i --to-stdout ./usr/bin/xlui > "$work/rpm-$arch"
  cmp "$binary" "$work/rpm-$arch"

  install -m 0755 "$binary" "$output/xlui-linux-$arch"
  cp "$deb" "$rpm_file" "$output/"
done
cp dist/SHA256SUMS "$output/"
cd "$output"
[[ "$(wc -l < SHA256SUMS)" -eq 6 ]]
sha256sum --strict --check SHA256SUMS
./xlui-linux-amd64 --help
