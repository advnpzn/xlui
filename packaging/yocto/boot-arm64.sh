#!/usr/bin/env bash
# Boot the recipe's installed executable inside a disposable Yocto reference image.
set -euo pipefail
binary=$(realpath "${1:?staged xlui executable}")
listing=$(realpath "${2:?xl list -v fixture}")
harness=$(realpath "$(dirname "$0")/verify-serial.py")
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
cd "$work"

base=https://downloads.yoctoproject.org/releases/yocto/yocto-6.0/machines/qemu/qemuarm64
rootfs=core-image-minimal-qemuarm64.rootfs-20260423205436.ext4.zst
for file in Image "$rootfs"; do
  curl --fail --location --silent --show-error --retry 2 --max-time 120 "$base/$file" -o "$file"
done
sha256sum -c <<SUMS
44e621e7ad968787de9f7cf1f1d260d8486b6431814608fac3a331eef01e3bdc  Image
18db5cb8d8c27659369e3f3dc92581cf9abd50c04645a2dec14895d440967fb3  $rootfs
SUMS

zstd -dc "$rootfs" > root.ext4
truncate -s +32M root.ext4
resize2fs -f root.ext4
install -m 0755 "$binary" xlui
cp "$listing" inventory.txt
cat > xl <<'SH'
#!/bin/sh
[ "$#" -eq 2 ] && [ "$1" = list ] && [ "$2" = -v ] || exit 1
cat /opt/xlui-test/inventory.txt
SH
chmod 0755 xl
debugfs -w -f - root.ext4 <<'CMDS'
mkdir /opt
mkdir /opt/xlui-test
write xlui /usr/bin/xlui
write inventory.txt /opt/xlui-test/inventory.txt
write xl /opt/xlui-test/xl
CMDS

/usr/bin/python3 "$harness" qemu-system-aarch64 \
  -machine virt,accel=tcg -cpu cortex-a57 -smp 2 -m 384 \
  -display none -monitor none -serial stdio -nic none -no-reboot \
  -kernel Image -append 'root=/dev/vda rw rootwait console=ttyAMA0,115200' \
  -drive file=root.ext4,if=none,format=raw,id=root \
  -device virtio-blk-pci,drive=root
