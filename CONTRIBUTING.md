# Contributing to xlui

Use the Go version in [go.mod](go.mod) or newer on Linux. Read the [source layout](docs/architecture.md) when choosing a package and the [TUI design](docs/tui.md) when adding a widget or screen.

## Build and run

From the repository root, run the same basic checks as CI.

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -o ./xlui ./cmd/xlui
./xlui --help
```

The tests use temporary executables to exercise local and SSH argument handling. They run without a Xen host. To exercise the current command on an authorized host, run `./xlui list` in Dom0 or `./xlui --host <ssh-alias> list` on a workstation. The remote path uses the workstation's OpenSSH client and the target's installed xl.

## Run the ARM64 Xen Dom0 smoke test

On a Debian 13 workstation, install `qemu-system-arm`, `python3-pexpect`, `cpio`, `gzip`, `xz-utils`, `openssh-client`, and `debian-archive-keyring`. From the repository root, run the following as a regular user with the Go toolchain on PATH.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/xlui-arm64 ./cmd/xlui
bash packaging/xen/boot-arm64.sh /tmp/xlui-arm64
```

The script downloads Debian trixie ARM64 packages using isolated APT state and Debian archive signatures, then boots the packaged Xen hypervisor and Linux control domain under QEMU software emulation. It compares the Dom0 name and UUID from native `xl list -v` against xlui's local and SSH inventory, opens that domain's inspector and vCPU placement through each connection, and checks that the TUI exits cleanly. QEMU has no network device. SSH uses loopback and a per-run pinned host key.

The test then creates a disposable Linux guest from the local TUI and edits its saved memory over SSH. It checks that the running allocation stays at 192 MiB, stops the identified test guest, and starts it again from the saved definition. A fresh guest console readiness marker, the original UUID, and a 224 MiB allocation confirm the second boot. After the restart, the test hot-plugs a Xen network interface into the guest and compares the local and SSH network inspectors with native `xl network-list`. The interface has no bridge or external uplink. The harness uses 1200 MiB of emulated RAM and removes its temporary files on exit. The captured `list-v-debian-4.20-arm64.txt` inventory also runs in the ordinary Go tests.

## Compare XAPI network inspection with a live pool

On an authorized XCP-ng or XenServer pool, capture the interfaces of one VM with at least one configured VIF. Run this with your existing authenticated `xe` client. Keep the VM configuration and power state stable during the comparison.

```sh
xe vif-list vm-uuid="${VM_UUID:?set VM_UUID to the selected guest UUID}" params=vm-uuid,uuid,device,MAC,currently-attached,network-uuid > xe-vifs.txt
```

Place the capture on your development workstation. Set `XLUI_XAPI_ENDPOINT` to the same pool's HTTPS coordinator, `XLUI_XAPI_USER` to a read-only account, and optionally `XLUI_XAPI_CA` to its private CA bundle. Run from the repository root in Bash.

```bash
(
  read -rs -p 'XAPI password: ' XLUI_XAPI_PASSWORD && echo
  export XLUI_XAPI_PASSWORD
  go test -count=1 -v -run '^TestLivePoolNetwork$' ./cmd/xlui -args -xe-vifs=TEXT_FILE
)
```

Replace `TEXT_FILE` with the absolute path to your capture. Export the endpoint, username and optional CA before running the command. The check opens the same inventory and network readers as the inspector, finds the VM by UUID and compares the complete set of VIF UUIDs, devices, MACs, network UUIDs and attachment states. It only reads the pool. Repeat with a running VM and a halted VM to cover both. Record the pool product, version, VM state and outcome when reporting compatibility. Keep the capture private.

## Where to test a change

| Change | Evidence to include in the pull request |
| --- | --- |
| Xen output or monitoring | Put a sanitized sample beside its parser under `testdata/`, with the command, Xen/tool version and expected fields. Cover the failure that matters, such as a missing field, counter reset or domain replacement. |
| Local or SSH execution | Exercise the real argument boundary with a temporary executable. Check cancellation, stderr or exit status when the change affects them. If tested on a host, identify the transport used. |
| Screen or key binding | Exercise the view with a snapshot or simulated terminal. For a layout change, include a screenshot or text rendering, ideally also at 80×24. |
| Host-changing action | Cover target identity and failure handling. When live testing is appropriate, use a disposable guest and report the observed result. Distinguish a command accepted by the toolstack from a state change verified on the host. |
| Packaging or target support | State which architecture and distribution were built and which actually ran. Cross-compilation, package installation and a live Xen session establish different things. |

Remove hostnames, guest identifiers, addresses, credentials and customer-specific paths from fixtures. Replace identifiers consistently when a relationship matters to the test.

## Submit a change

1. Prepare the change locally. Keep each commit focused on one user-visible behavior or one coherent internal change.
2. Format touched Go files with `gofmt` and run the relevant checks. Use Conventional Commits, for example `feat(tui): filter domains by state` or `fix(xl): preserve unknown CPU readings`.
3. Maintainers can apply a verified change directly to the current default branch. The Go workflow checks pull requests and default-branch pushes that change Go code, fixtures, dependencies or build automation. It can also run manually. Contributors using a fork or requesting review can open a pull request against that branch with a Conventional Commit title. Describe the operator-visible effect, verification and any changes to privileges, target identity or compatibility.

For a bug report, include the command or keystrokes, expected and observed behavior, xlui revision, host toolstack version and connection mode. For a new operation, describe the operator's task and which host management stack owns it.

## Release Linux packages

Push a semantic version tag such as `v1.2.3` or `v1.2.3-rc.1` on the commit to release. **Linux packages** runs the Go checks first, builds twice, compares checksums and inspects the DEB and RPM payloads. It runs the x86-64 client natively and the ARM64 client under QEMU against an inventory fixture.

The workflow attaches the six verified downloads and `SHA256SUMS` to a draft GitHub release, checks their uploaded digests, then publishes the release notes. Tags with a prerelease suffix publish as prereleases. Rerun a failed tag workflow to retry. If the release tooling needs a fix, run **Linux packages** on the corrected default branch and enter the existing tag in `release_tag`. This tests and builds the original tagged source with the corrected packaging configuration. Leave the input empty on a branch for a snapshot build.
