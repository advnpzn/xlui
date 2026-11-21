"""Check a disposable Xen Dom0 using its logged-in serial console."""

import re
import sys
import tempfile

import pexpect

UUID = r"[a-fA-F0-9]{8}(?:-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}"


def shell(console, command):
    # The anchored markers cannot match the console's echoed command.
    console.sendline("printf '\\nXLCHECK-BEGIN\\n'; " + command +
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


def check_dom0(console):
    shell(console, "stty rows 32 cols 100 && export TERM=xterm-256color")
    shell(console, "grep -qw control_d /proc/xen/capabilities")
    native = shell(console, "xl list -v")
    name, uuid = control_domain(native)

    # Model a workstation without xl. An SSH command routed locally must fail.
    shell(console, "mkdir -p /run/workstation-bin && ln -s /usr/bin/ssh /run/workstation-bin/ssh")
    clients = ["/usr/bin/xlui", "PATH=/run/workstation-bin /usr/bin/xlui --host dom0-test"]
    for client in clients:
        print(f"Checking {client}", flush=True)
        observed = control_domain(shell(console, client + " list"))
        if observed != (name, uuid):
            raise RuntimeError(f"{client} observed {observed}; xl observed {(name, uuid)}")

        console.sendline(client + " --read-only --interval 10s; printf '\\nxlui-exit=%d\\n' \"$?\"")
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
        console.send("q")
        console.expect(r"(?:^|[\r\n])xlui-exit=0\r*\n")
    return native


if __name__ == "__main__":
    with tempfile.TemporaryFile(mode="w+", encoding="utf-8") as log, pexpect.spawn(
        sys.argv[1], sys.argv[2:], encoding="utf-8", codec_errors="replace",
        timeout=30, dimensions=(32, 100),
    ) as console:
        console.logfile_read = log
        try:
            console.expect_exact("XLUI-DOM0-READY", timeout=180)
            print(check_dom0(console))
        except (pexpect.TIMEOUT, pexpect.EOF):
            log.seek(0)
            sys.exit(f"Serial check failed. Last console output: {log.read()[-3000:]!r}")
    print("Verified live ARM64 Xen Dom0 inventory, inspector and vCPU placement over local xl and OpenSSH.")
