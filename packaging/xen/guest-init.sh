#!/bin/busybox sh
set -eu
export PATH=/usr/lib/xen-4.20/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root TERM=xterm
mount -t devtmpfs devtmpfs /dev
exec </dev/hvc0 >/dev/hvc0 2>&1
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mkdir -p /dev/pts /dev/shm
mount -t devpts devpts /dev/pts
mount -t tmpfs tmpfs /dev/shm
for module in xen-evtchn xen-privcmd xen-gntdev xenfs xen-netback; do
  /bin/busybox insmod "/modules/$module.ko"
done
mount -t xenfs xenfs /proc/xen
xenstored --pid-file /run/xenstored.pid
for attempt in 1 2 3 4 5; do
  /bin/busybox timeout 2 xenstore-read -s / >/dev/null && break
  sleep 1
done
xen-init-dom0
xenconsoled --log guest --log-dir /var/log/xen
/bin/busybox ip link set lo up
/usr/sbin/sshd -f /etc/ssh/test_sshd_config
echo XLUI-DOM0-READY
exec /bin/busybox setsid /bin/busybox cttyhack /bin/sh -i
