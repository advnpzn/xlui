#!/usr/bin/env bash
# Build just xlui's BitBake tasks with an installed Go toolchain. The full
# distribution build uses OE's own compiler and sysroot instead.
set -eo pipefail
oe=$(realpath "${1:?OE-Core checkout containing bitbake/}")
source_repo=$(realpath "${2:?local Git checkout containing the recipe source revision}")
build=$(realpath -m "${3:?empty build directory}")
layer=$(realpath "$(dirname "$0")/meta-xlui")
recipe="$layer/recipes-extended/xlui/xlui_git.bb"
goroot=$(go env GOROOT)
gotools=$(go env GOTOOLDIR)
if [[ -e "$build" ]]; then
  echo "Use a new build directory: $build" >&2
  exit 1
fi

source "$oe/oe-init-build-env" "$build"
set -u
cat >> conf/local.conf <<CONF
MACHINE = "qemuarm64"
XLUI_SRC_URI = "git://$source_repo;protocol=file;nobranch=1"
HOSTTOOLS += "go"
GO:pn-xlui = "go"
GOROOT:pn-xlui = "$goroot"
GOTOOLDIR:pn-xlui = "$gotools"
CONF
echo "BBLAYERS += \"$layer\"" >> conf/bblayers.conf

# -b executes the recipe without bootstrapping the distribution toolchain.
# Fetch is the only task that should require the network.
bitbake -b "$recipe" -c fetch
echo 'BB_NO_NETWORK = "1"' >> conf/local.conf
# go-mod schedules compile before the dependency license checks.
bitbake -b "$recipe" -c populate_lic

# The install body copies a single binary and needs no fakeroot ownership.
bitbake-getvar -r xlui --value do_install | bash -eu
image=$(bitbake-getvar -r xlui --value D)
binary="$image/usr/bin/xlui"
header=$(readelf -h "$binary")
segments=$(readelf -lW "$binary")
dynamic=$(readelf -dW "$binary")
[[ "$header" == *AArch64* && "$segments" != *INTERP* && "$dynamic" != *NEEDED* ]]
qemu-aarch64 "$binary" --help

# Use the fixture from the source revision pinned in the recipe.
src=$(bitbake-getvar -r xlui --value S)
export XLUI_TEST_LISTING="$src/src/github.com/advnpzn/xlui/internal/xen/xl/testdata/list-v-xen-4.19.txt"
mkdir -p "$build/stubs"
cat > "$build/stubs/xl" <<'SH'
#!/bin/sh
[ "$#" -eq 2 ] && [ "$1" = list ] && [ "$2" = -v ] || exit 1
cat "$XLUI_TEST_LISTING"
SH
chmod 0755 "$build/stubs/xl"
PATH="$build/stubs:$PATH" qemu-aarch64 "$binary" list > "$build/list.txt"
cmp "$XLUI_TEST_LISTING" "$build/list.txt"

# Pass only the staged executable and its matching fixture to the boot runner.
mkdir -p "$build/serial"
install -m 0755 "$binary" "$build/serial/xlui"
cp "$XLUI_TEST_LISTING" "$build/serial/inventory.txt"
