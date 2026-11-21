# xlui

xlui is a **terminal user interface (TUI) for Xen**. Monitor multiple Xen hosts from one terminal, follow CPU and memory usage by domain, and switch between saved local, SSH and XenAPI connections with the keyboard or mouse.

## Installation

Open **Actions → Linux packages → Run workflow**, or select a build for a version tag. Download the `xlui-linux-<commit>` artifact and extract its ZIP. It contains static Linux binaries, DEB and RPM packages for amd64 and arm64, and `SHA256SUMS`. From the extracted directory, verify the files with `sha256sum -c SHA256SUMS`. Install the package for your machine with your distribution's package manager, or run `chmod +x xlui-linux-arm64` before using the standalone ARM binary. For x86-64 use `xlui-linux-amd64`.

To install from source on Linux, use Git and Go 1.25.0 or newer. Use Git credentials with access to the repository.

```sh
git clone https://github.com/advnpzn/xlui.git
cd xlui
CGO_ENABLED=0 GOBIN="$HOME/.local/bin" go install -trimpath ./cmd/xlui
export PATH="$HOME/.local/bin:$PATH"
xlui --help
```

If `~/.local/bin` is not already on your `PATH`, add the export line to your shell profile.

## Setup

**Local Xen host.** Run xlui in Dom0 as an account that can run `xl list -v`. The TUI uses the same account to collect inventory.

**Remote Xen host.** Install xlui and the OpenSSH client on your workstation. The remote Dom0 needs `xl` and an account with permission to list domains. Verify the host key and configure key-based authentication so the TUI can refresh without prompting. You can put the user, port or jump host in an SSH config alias. The following alias belongs in `~/.ssh/config`.

```sshconfig
Host bench
    HostName xen-host.example
    User operator
```

Replace the hostname and user with your own. To check the connection and permissions before opening the TUI, run this command.

```sh
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes bench xl list -v
```

## Using the TUI

Run `xlui` to open the local domain list, or `xlui --host bench` to use the SSH alias above.

### Connect more hosts

Press **s** or click **Sessions**, then choose **Add connection**. Give the connection a name and choose how xlui should reach it.

| Connection | What to enter |
| --- | --- |
| Local Dom0 | A name for the Xen host where xlui runs |
| SSH | An OpenSSH alias or `user@host`. Configure keys, ports and jump hosts in `~/.ssh/config`. |
| XenAPI | The HTTPS pool coordinator, username and optionally a private CA bundle. xlui prompts for a password when you connect. |

Choose **Save + open** to start sampling, or **Save** to connect later. Press **Tab** on the dashboard to cycle through open sessions. Each session keeps sampling in the background and retains its own selection, filter and charts. Open a saved connection in **Sessions** to edit it or disconnect. After a restart, open the saved profiles you need.

xlui writes profiles to `$XDG_CONFIG_HOME/xlui/config.json`, normally `~/.config/xlui/config.json`, with owner-only file permissions. Passwords stay in memory. To start directly on a pool, run `xlui --xapi https://pool.example --xapi-user viewer`.

### Read the dashboard

| Reading | Meaning |
| --- | --- |
| CPU load | 100% is one fully busy vCPU. A four-vCPU guest can reach 400%. Local and SSH sessions need two successful samples to calculate load. |
| Allocated | The amount of memory Xen assigns to the domain. The summary adds the allocations of reporting domains. |
| Guest used | On local and SSH connections, xlui reads total minus available memory in Dom0 and total minus reported free memory from each guest’s XenStore agent. XenAPI uses its RRD allocation and guest-free readings. |
| History | The selected domain shows recent CPU and guest-used memory. The top charts show CPU and allocated memory for this session. Gaps indicate unavailable measurements. |

A dash means the provider has no reading. Guest memory requires a reporting guest agent and permission to read its metrics. XenAPI lists VMs across the pool. CPU and guest memory charts cover the VM series served by the configured endpoint. [XenServer metrics documentation](https://docs.xenserver.com/us/en/xenserver/developer/sdk-guide/using-http.html)

### Navigation

Mouse controls are enabled automatically in terminals that support mouse reporting. The buttons at the bottom of the screen also show their keyboard shortcuts.

| Action | Keyboard | Mouse |
| --- | --- | --- |
| Manage connections | `s` | Click **Sessions** |
| Switch open sessions | Tab | Click **Next** |
| Select a domain | ↑ / ↓ | Click its row |
| Open the inspector | Enter | Double-click a row or click **Inspect** |
| Scroll the list or inspector | ↑ / ↓ or Page Up / Page Down | Scroll the wheel over the list or inspector |
| Return to the list | Esc | Click **Back** |
| Filter by name, ID, UUID or state | `/`, then type | Click **Filter**, then type |
| Apply or cancel a filter edit | Enter / Esc | Click **Apply** or **Cancel** |
| Clear the applied filter | Esc in the list | Click **Clear** |
| Quit | `q` outside the filter editor, or Ctrl+C anywhere | Click **Quit** |

The list refreshes five seconds after each collection. Use `xlui --host bench --interval 10s` to change the delay. If a refresh fails, **Stale** and a timestamp identify the last successful sample. Open the inspector to see cumulative CPU time where available and up to 60 samples of CPU and allocated memory. Inspector charts start when opened and mark gaps with `?`.

## Headless inventory

For a one-time listing or a script, use `list`. Redirecting plain-Xen output preserves the original `xl` listing.

```sh
xlui list
xlui --host bench list
```

### XenAPI pools

The `list` command can also query a pool coordinator over HTTPS. It lists running and halted VMs, identifies control domains and excludes templates and snapshots. Use an account with permission to read VM records. In Bash, enter the password at the prompt.

```bash
read -rs -p 'XAPI password: ' pool_password && echo
XLUI_XAPI_PASSWORD="$pool_password" xlui --xapi https://pool.example --xapi-user viewer list
unset pool_password
```

The coordinator must support XenAPI JSON-RPC 2.0 at `/jsonrpc`. xlui trusts system certificate authorities by default. For a private CA, add `--xapi-ca pool-ca.pem` before `list`. Compatibility with individual XCP-ng and XenServer releases still needs live verification.

## Building from source

From the repository root, run the tests and build a standalone Linux binary.

```sh
go test ./...
CGO_ENABLED=0 go build -trimpath -o xlui ./cmd/xlui
./xlui --help
```

CI builds static binaries for Linux amd64 and arm64. To build for an arm64 Xen host from another Linux machine, set the target explicitly.

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o xlui-linux-arm64 ./cmd/xlui
```

## Project documentation

- [Management actions](docs/architecture.md#management-actions) covers planned guest creation, configuration edits, power controls and deletion.
- [TUI design](docs/tui.md) describes screens, widgets and keyboard interactions.
- [Architecture](docs/architecture.md) explains providers, transport and session handling.
- [Contributing](CONTRIBUTING.md) covers local checks, test fixtures and contributions.
