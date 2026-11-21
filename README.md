# xlui

This development snapshot implements a read-only domain inventory command for a local Xen control domain or a remote one reached through OpenSSH. It streams the host's verbose xl listing and preserves command failures. This snapshot has no interactive TUI or mutation commands.

Build with Go 1.22 or newer on Linux.

```sh
go test ./...
CGO_ENABLED=0 go build -o xlui ./cmd/xlui
./xlui --host ssh://operator@bench list
./xlui list
```

The remote host needs a functioning xl installation and an account permitted to list domains. The workstation needs an OpenSSH client. SSH config aliases can carry port and jump host settings. The local command uses the current account and environment to run xl. No automatic privilege escalation is performed.

## Working on xlui

- [Architecture and source layout](docs/architecture.md) explains where domain data, terminal views, transports and host actions belong.
- [TUI design](docs/tui.md) defines reusable widgets, Xen-specific screens and the session interfaces that connect them.
- [Contributing](CONTRIBUTING.md) covers local checks, test fixtures and Conventional Commits.
