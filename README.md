# xlui

xlui is a terminal management tool for Xen, inspired by k9s. The project scope includes inspecting, creating, editing, starting, stopping and deleting guests through the toolstack that manages them.

The current implementation supports inventory and inspection. Use the interactive browser locally in Dom0 or from a Linux workstation over OpenSSH. The headless `list` command also supports XenAPI pools. Guest lifecycle and configuration changes are planned in the [management action design](docs/architecture.md#management-actions).

Build on Linux with Go 1.25.0 or newer. Use the latest patched stable Go release for production builds, as CI does.

```sh
go test ./...
CGO_ENABLED=0 go build -o xlui ./cmd/xlui
./xlui
./xlui --host bench --interval 5s
./xlui --host ssh://operator@bench list
./xlui list
```

The remote host needs a functioning xl installation and an account permitted to list domains. The workstation needs an OpenSSH client. SSH config aliases can carry port and jump host settings. The local command uses the current account and environment to run xl. No automatic privilege escalation is performed.

For the SSH browser, verify the host key and configure unattended authentication for the destination first. Background reads use OpenSSH batch mode with strict host-key checking. Inventory commands disable agent, X11 and port forwarding.

The browser samples `xl list -v` once per refresh. A failed refresh keeps the last successful sample visible with its timestamp and a **Stale** status. The default refresh delay is five seconds after each collection and can be changed with `--interval`. CPU time is cumulative seconds across vCPUs.

| In the browser | Keys |
| --- | --- |
| Select a domain and open its inspector | Up/Down, then Enter |
| Filter domains | `/` to edit, Enter to apply, Esc to cancel an edit |
| Clear the applied filter from the list | Esc |
| Scroll the inspector or return to the list | Up/Down or Page Up/Page Down to scroll, Esc to return |
| Quit | `q` outside the filter editor, or Ctrl+C anywhere |

The plain-Xen `list` command preserves original bytes when redirected to a file or pipe. When stdout or stderr is a terminal, xlui replaces control characters except for the tabs and line feeds used for layout.

## XAPI pool inventory

Point `--xapi` at the HTTPS pool coordinator and use an account permitted to read VM records. In Bash, enter the password at the prompt and pass it only to xlui.

```bash
read -rs -p 'XAPI password: ' pool_password && echo
XLUI_XAPI_PASSWORD="$pool_password" ./xlui --xapi https://pool.example --xapi-user viewer list
unset pool_password
```

System certificate roots are used by default. Add `--xapi-ca pool-ca.pem` for a private CA. A single `VM.get_all_records` call lists running and halted VMs across the pool, identifies control domains, and filters templates and snapshots. Login and logout each cost one additional request. The client requires XenAPI JSON-RPC 2.0 at `/jsonrpc` and accepts inventory responses up to 32 MiB. Tests exercise a TLS fixture server. Compatibility with individual XCP-ng and XenServer releases still needs a live pool test.

## Working on xlui

- [Architecture and source layout](docs/architecture.md) explains where domain data, terminal views, transports and host actions belong.
- [TUI design](docs/tui.md) defines reusable widgets, Xen-specific screens and the session interfaces that connect them.
- [Contributing](CONTRIBUTING.md) covers local checks, test fixtures and Conventional Commits.
