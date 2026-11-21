# xlui

xlui is a read-only terminal browser for plain Xen hosts. Use it locally in Dom0 or connect from a Linux workstation over OpenSSH. It also provides headless VM inventory for XenAPI pools.

Build with Go 1.22 or newer on Linux.

```sh
go test ./...
CGO_ENABLED=0 go build -o xlui ./cmd/xlui
./xlui
./xlui --host bench --interval 5s
./xlui --host ssh://operator@bench list
./xlui list
```

The remote host needs a functioning xl installation and an account permitted to list domains. The workstation needs an OpenSSH client. SSH config aliases can carry port and jump host settings. The local command uses the current account and environment to run xl. No automatic privilege escalation is performed.

The browser samples `xl list -v` once per refresh and labels the last good sample stale after a failed read. Use the arrow keys to select a domain, `/` to filter, Enter to inspect the current sample, Esc to go back and `q` to quit. CPU time is cumulative seconds across vCPUs. The default refresh delay is five seconds after each collection and can be changed with `--interval`. The `list` subcommand still streams the plain-Xen listing for scripts.

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
