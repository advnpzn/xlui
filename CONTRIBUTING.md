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
3. Maintainers can apply a verified change directly to the current default branch. Launch the Go workflow manually from Actions when a full CI run is needed. Contributors using a fork or requesting review can open a pull request against that branch with a Conventional Commit title. Describe the operator-visible effect, verification and any changes to privileges, target identity or compatibility.

For a bug report, include the command or keystrokes, expected and observed behavior, xlui revision, host toolstack version and connection mode. For a new operation, describe the operator's task and which host management stack owns it.
