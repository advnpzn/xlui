"""Check a disposable Xen Dom0 using its logged-in serial console."""

import re
import sys
import tempfile
import time

import pexpect

UUID = r"[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}"


def send_line(console, line):
    # Leave time for the emulated Xen serial console to drain its input ring.
    for start in range(0, len(line), 24):
        console.send(line[start:start + 24])
    console.sendline("")


def shell(console, command):
    # The anchored markers cannot match the console's echoed command.
    send_line(console, "printf '\\nXLCHECK-BEGIN\\n'; " + command +
                     "; printf '\\nXLCHECK-END=%d\\n' \"$?\"")
    console.expect(r"(?:^|[\r\n])XLCHECK-BEGIN\r*\n")
    console.expect(r"(?:^|[\r\n])XLCHECK-END=(\d+)\r*\n")
    output = console.before.replace("\r", "").strip("\n")
    if console.match.group(1) != "0":
        raise RuntimeError(f"{command} failed: {output}")
    return output


def control_domain(listing):
    for line in listing.splitlines():
        # CPU time and run state legitimately change between live reads.
        row = re.fullmatch(r"(.+?)\s+0\s+\d+\s+\d+\s+\S+\s+[\d.]+\s+(" +
                           UUID + r")(?:\s+.*)?", line)
        if row:
            return row.group(1).strip(), row.group(2).lower()
    raise RuntimeError(f"No control domain with a UUID in xl inventory:\n{listing}")


LOCAL = "/usr/bin/xlui"
SSH = "PATH=/run/workstation-bin /usr/bin/xlui --host dom0-test"


def check_dom0(console):
    shell(console, "stty rows 32 cols 100 && export TERM=xterm-256color")
    shell(console, "grep -qw control_d /proc/xen/capabilities")
    native = shell(console, "xl list -v")
    name, uuid = control_domain(native)

    # Model a workstation without xl. An SSH command routed locally must fail.
    shell(console, "mkdir -p /run/workstation-bin && ln -s /usr/bin/ssh /run/workstation-bin/ssh")
    for client in (LOCAL, SSH):
        print(f"Checking {client}", flush=True)
        observed = control_domain(shell(console, client + " list"))
        if observed != (name, uuid):
            raise RuntimeError(f"{client} observed {observed}; xl observed {(name, uuid)}")

        send_line(console, client + " --read-only --interval 10s; printf '\\nxlui-exit=%d\\n' \"$?\"")
        console.expect_exact(name)
        # This disposable guest has only its control domain.
        console.send("\r")
        console.expect_exact("UUID")
        console.expect_exact(uuid)
        console.send("v")
        console.expect_exact("Hard: all")
        console.expect_exact("Soft: all")
        console.send("\x1b")
        console.expect_exact("UUID")
        console.send("n")
        console.expect_exact("NETWORK INTERFACES")
        console.expect_exact("No interfaces reported by xl.")
        console.send("\x1b")
        console.expect_exact("UUID")
        console.send("q")
        console.expect(r"(?:^|[\r\n])xlui-exit=0\r*\n")
    return native


def guest_action(console, client, key, menu_item, title, fields, preview):
    send_line(console, client + " --interval 2s; printf '\\nxlui-exit=%d\\n' \"$?\"")
    console.expect_exact("Domain-0")
    console.send("g")
    console.expect_exact(menu_item)
    console.send(key)
    console.expect_exact(title)
    for field in fields:
        console.send(field)
    console.expect_exact("REVIEW GUEST CHANGE")
    console.expect_exact(preview)
    console.send("\t\r")
    console.expect_exact("GUEST UPDATED", timeout=90)
    console.send("\r")
    console.expect_exact("xlui-lab")
    console.send("q")
    console.expect(r"(?:^|[\r\n])xlui-exit=0\r*\n")


def guest_id(console, uuid, memory):
    listing = shell(console, "xl list -v")
    row = re.search(rf"^xlui-lab\s+([1-9]\d*)\s+{memory}\s+1\s+\S+\s+[\d.]+\s+{uuid}\s", listing, re.M)
    if not row:
        raise RuntimeError(f"Guest {uuid} with {memory} MiB and one vCPU missing from {listing}")
    return int(row[1])


def wait_for_linux(console):
    shell(console, "i=0; until /bin/busybox grep -q XLUI-DISPOSABLE-GUEST-READY "
          "/var/log/xen/guest-xlui-lab.log 2>/dev/null; do "
          "i=$((i+1)); [ $i -lt 25 ] || break; sleep 1; done; [ $i -lt 25 ]")


def check_guest_inspectors(console, domid, uuid):
    mac = "00:16:3e:00:00:42"
    shell(console, f"xl network-attach {domid} mac={mac} script=/etc/xen/scripts/vif-xlui-test")
    deadline = time.monotonic() + 30
    # xl columns: index, backend, MAC, handle, frontend state, event channel,
    # ring references and backend path. Wait for both halves of the device.
    connected = re.compile(rf"^\s*(\d+)\s+0\s+{mac}\s+\d+\s+4\s+\S+\s+\S+\s+(\S+)\s*$", re.M)
    while True:
        listing = shell(console, f"xl network-list {domid}")
        row = connected.search(listing)
        if row:
            vif, path = row.groups()
            if path != f"/local/domain/0/backend/vif/{domid}/{vif}":
                raise RuntimeError(f"Unexpected backend for test VIF: {path}")
            if shell(console, f"xenstore-read {path}/state") == "4":
                break
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Test VIF did not connect on domain {domid}:\n{listing}")
        time.sleep(1)
    print(listing, flush=True)

    # Stand in for guest tools on this diskless image. The synthetic counters
    # represent 1536 KiB used; the current-value badge rounds this to 2 MiB.
    shell(console, f'p=/local/domain/{domid}; test "$(xenstore-read "$p/vm")" = /vm/{uuid} '
          '&& xenstore-write "$p/data/meminfo_total" 4096 "$p/data/meminfo_free" 2560')
    for client in (LOCAL, SSH):
        print(f"Checking synthetic guest memory and the connected VIF with {client}", flush=True)
        send_line(console, client + " --read-only --interval 10s; printf '\\nxlui-exit=%d\\n' \"$?\"")
        console.expect_exact("xlui-lab")
        # Select the guest from the two-domain inventory, then check its identity.
        console.send("\x1b[B\r")
        console.expect_exact(uuid)
        # A terminal can move its cursor between the value and unit.
        console.expect(r"1\.5(?: |\x1b\[[0-?]*[ -/]*[@-~])+MiB")
        console.send("n")
        console.expect_exact("NETWORK INTERFACES")
        console.expect_exact(f"VIF {vif}")
        console.expect_exact(f"MAC {mac}")
        console.expect_exact("Backend domain 0")
        console.expect_exact("Xenbus state Connected (4)")
        console.expect_exact(path)
        console.send("\x1b")
        console.expect_exact("UUID")
        console.send("q")
        console.expect(r"(?:^|[\r\n])xlui-exit=0\r*\n")


def check_guest_lifecycle(console):
    config = "/etc/xen/xlui-lab.cfg"
    shell(console, f"test ! -e {config} && ! xl domid xlui-lab >/dev/null 2>&1")
    print("Creating a diskless Linux guest through the local TUI", flush=True)
    guest_action(console, LOCAL, "c", "Create guest", "CREATE GUEST",
                 ["xlui-lab\t", "/guest/Image\t", "/guest/initrd.gz\t", "\t", "\x15192\t", "\t\r"],
                 "192")
    original = shell(console, f"cat {config}")
    identity = re.search(rf'^uuid = "({UUID})"$', original, re.M)
    if not identity or "memory = 192" not in original.splitlines():
        raise RuntimeError(f"Unexpected new definition: {original}")
    uuid = identity[1]
    first = guest_id(console, uuid, 192)
    wait_for_linux(console)

    print("Changing the saved boot memory over SSH", flush=True)
    guest_action(console, SSH, "b", "Edit saved boot memory", "EDIT GUEST", ["xlui-lab\t", "224\t\r"], "224")
    if shell(console, f"cat {config}") != original.replace("memory = 192", "memory = 224"):
        raise RuntimeError("Unexpected change to the saved definition")
    if guest_id(console, uuid, 192) != first:
        raise RuntimeError("The running guest changed during the edit")

    # This QEMU instance is private. Stop only the DomU we just created and checked.
    shell(console, f"xl destroy {first}")
    shell(console, "! xl domid xlui-lab >/dev/null 2>&1 && rm /var/log/xen/guest-xlui-lab.log")
    print("Starting the saved definition over SSH", flush=True)
    guest_action(console, SSH, "s", "Start from saved definition", "START FROM DEFINITION",
                 ["xlui-lab\t", "\r"], uuid)
    second = guest_id(console, uuid, 224)
    if second == first:
        raise RuntimeError("Expected a newly booted domain")
    wait_for_linux(console)
    check_guest_inspectors(console, second, uuid)


if __name__ == "__main__":
    with tempfile.TemporaryFile(mode="w+", encoding="utf-8") as log, pexpect.spawn(
        sys.argv[1], sys.argv[2:], encoding="utf-8", codec_errors="replace",
        timeout=30, dimensions=(32, 100),
    ) as console:
        console.logfile_read = log
        try:
            console.expect_exact("XLUI-DOM0-READY", timeout=180)
            console.expect_exact("/ # ")
            print(check_dom0(console))
            check_guest_lifecycle(console)
        except (pexpect.TIMEOUT, pexpect.EOF):
            log.seek(0)
            sys.exit(f"Serial check failed. Last console output: {log.read()[-3000:]!r}")
    print("Verified live ARM64 Xen Dom0 inventory, inspector and vCPU placement, Linux guest creation, saved memory edits and restart, and synthetic guest memory and a connected VIF through the local and SSH inspectors.")
