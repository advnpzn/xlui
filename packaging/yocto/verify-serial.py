#!/usr/bin/env python3
"""Boot a disposable qemuarm64 image and exercise xlui over its serial console."""

import sys

import pexpect


def check_guest(console):
    # Poky reference images can prompt for a passwordless root login or autologin.
    prompt = r"(?:^|[\r\n])[^\r\n]*# "
    boot = console.expect([r"(?:^|[\r\n])[^\r\n]* login: ", prompt], timeout=180)
    if boot == 0:
        console.sendline("root")
        console.expect(prompt)

    # A UART has no window-size negotiation. Set the actual guest tty dimensions.
    console.sendline(
        "stty rows 32 cols 100 && export TERM=xterm-256color PATH=/opt/xlui-test:$PATH"
        " && test -x /usr/bin/xlui && printf 'xlui-arch=%s\\n' \"$(uname -m)\""
    )
    console.expect_exact("xlui-arch=aarch64")
    console.sendline(
        "/usr/bin/xlui --read-only --interval 10s; printf 'xlui-exit=%d\\n' \"$?\""
    )
    console.expect_exact("Domain-0")
    console.send("\r")
    console.expect_exact("UUID")
    console.send(",")
    console.expect_exact("SETTINGS")
    console.send("\x1b")
    console.expect_exact("UUID")
    console.send("q")
    console.expect_exact("xlui-exit=0")


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: verify-serial.py QEMU [ARGS...]")
    with pexpect.spawn(
        sys.argv[1], sys.argv[2:], encoding="utf-8", codec_errors="replace",
        timeout=30, dimensions=(32, 100),
    ) as console:
        try:
            check_guest(console)
        except (pexpect.TIMEOUT, pexpect.EOF) as exc:
            # Keep the failure diagnostic short, including recently consumed output.
            sys.exit(f"Serial check failed: {exc}")
    print("Arm64 guest booted; xlui inventory, inspector, settings and exit verified.")


if __name__ == "__main__":
    main()
