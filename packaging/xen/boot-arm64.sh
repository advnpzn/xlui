#!/usr/bin/env bash
# Assemble an unprivileged, disposable Debian Dom0 and boot it with real Xen.
set -euo pipefail
binary=$(realpath "${1:?path to static Linux arm64 xlui binary}")
scripts=$(realpath "$(dirname "$0")")
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
cd "$work"

# Keep the host package state and its set of enabled architectures untouched.
mkdir -p debs/partial lists/partial root
touch status
echo 'deb [arch=arm64 signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] https://deb.debian.org/debian trixie main' > sources.list
apt_args=(
  -o "Dir::Etc::sourcelist=$work/sources.list" -o Dir::Etc::sourceparts=-
  -o "Dir::State::lists=$work/lists" -o "Dir::State::status=$work/status"
  -o "Dir::Cache::archives=$work/debs" -o APT::Architecture=arm64
  -o APT::Architectures=arm64 -o APT::Install-Recommends=false
)
apt-get "${apt_args[@]}" update
apt-get "${apt_args[@]}" --download-only -y install linux-image-arm64 \
  xen-hypervisor-4.20-arm64 xen-utils-4.20 busybox-static openssh-server util-linux
for deb in debs/*.deb; do dpkg-deb -x "$deb" root; done

cd root
ln -s usr/bin bin
ln -s usr/lib lib
ln -s usr/sbin sbin
ln -s /run var/run
ln -s busybox usr/bin/sh
ln -s ../lib/xen-4.20/bin/xl usr/bin/xl
mkdir -p dev proc sys tmp run/{sshd,xen,xenstored} var/lib/{xen,xenstored} var/log/xen root/.ssh modules
install -m 0755 "$binary" usr/bin/xlui
install -m 0755 "$scripts/guest-init.sh" init
for module in xen-evtchn xen-privcmd xen-gntdev xenfs xen-netback; do
  xz -dc "$(find usr/lib/modules -name "$module.ko.xz")" > "modules/$module.ko"
done

# Bring up the isolated backend and complete Xen's hotplug handshake.
cat > etc/xen/scripts/vif-xlui-test <<'HOTPLUG'
#!/bin/busybox sh
set -eu
case "$1" in
  online)
    /bin/busybox ip link set "$vif" up
    /usr/bin/xenstore-write "$XENBUS_PATH/hotplug-status" connected
    ;;
  offline) exit 0 ;;
  *) exit 1 ;;
esac
HOTPLUG
chmod +x etc/xen/scripts/vif-xlui-test

# All keys and accounts exist only in the guest image. QEMU has no NIC.
printf '%s\n' 'root:x:0:0:root:/root:/bin/sh' 'sshd:x:220:220::/run/sshd:/bin/sh' > etc/passwd
printf '%s\n' 'root:x:0:' 'sshd:x:220:' > etc/group
echo 'root:zzINVALID:19730:0:99999:7:::' > etc/shadow
chmod 0600 etc/shadow
ssh-keygen -q -t ed25519 -N '' -f root/.ssh/id_ed25519
ssh-keygen -q -t ed25519 -N '' -f etc/ssh/ssh_host_ed25519_key
cp root/.ssh/id_ed25519.pub root/.ssh/authorized_keys
{ printf '[127.0.0.1]:2222 '; cat etc/ssh/ssh_host_ed25519_key.pub; } > root/.ssh/known_hosts
cat > root/.ssh/config <<'SSH'
Host dom0-test
 HostName 127.0.0.1
 Port 2222
 User root
 BatchMode yes
 StrictHostKeyChecking yes
SSH
chmod 0700 root/.ssh
chmod 0600 root/.ssh/config root/.ssh/authorized_keys
cat > etc/ssh/test_sshd_config <<'SSH'
Port 2222
ListenAddress 127.0.0.1
PermitRootLogin prohibit-password
PasswordAuthentication no
UsePAM no
HostKey /etc/ssh/ssh_host_ed25519_key
SSH

# The diskless test guest reuses Dom0's kernel and runs a static BusyBox init.
kernel=(boot/vmlinuz-*)
mkdir -p guest ../domu/{bin,dev,modules}
ln "${kernel[0]}" guest/Image
cp usr/bin/busybox ../domu/bin/
xz -dc "$(find usr/lib/modules -name xen-netfront.ko.xz)" > ../domu/modules/xen-netfront.ko
cat > ../domu/init <<'INIT'
#!/bin/busybox sh
set -eu
/bin/busybox mount -t devtmpfs devtmpfs /dev
/bin/busybox insmod /modules/xen-netfront.ko
echo XLUI-DISPOSABLE-GUEST-READY > /dev/hvc0
exec /bin/busybox sleep 3600
INIT
chmod +x ../domu/init
(cd ../domu && find . -print0 | cpio --quiet --null -o -H newc --owner=0:0) | gzip -1 > guest/initrd.gz

# Modules needed to bring up Xen were decompressed into /modules above.
find . \( -path './boot' -o -path './usr/share' -o -path './usr/lib/modules' \
  -o -path './usr/lib/linux-image-*' -o -path './usr/lib/python*' -o -path './usr/lib/systemd' \) \
  -prune -o -print0 | cpio --quiet --null -o -H newc --owner=0:0 | gzip -1 > ../initramfs.gz
python3 "$scripts/verify-dom0.py" qemu-system-aarch64 \
  -machine virt,virtualization=on,gic-version=3 -accel tcg -cpu cortex-a57 -smp 2 -m 1200 \
  -display none -monitor none -serial stdio -nic none -no-reboot \
  -kernel boot/xen-4.20-arm64 -append 'dom0_mem=768M,max:768M dom0_max_vcpus=1 console=dtuart dtuart=serial0' \
  -device "guest-loader,addr=0x44000000,kernel=${kernel[0]},bootargs=console=hvc0 rdinit=/init rootfstype=ramfs" \
  -device guest-loader,addr=0x51000000,initrd=../initramfs.gz
