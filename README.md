# xlui

xlui is a **terminal user interface (TUI) for Xen**. Monitor multiple Xen hosts, create guests, and edit boot memory and vCPUs from the keyboard or mouse. Switch between saved local, SSH and XenAPI connections while each session keeps sampling.

## Installation

Open the release you want and download `SHA256SUMS` and one Linux file from **Assets** into an empty directory. Use `uname -m` to identify your architecture. [GitHub Releases](https://github.com/advnpzn/xlui/releases)

| `uname -m` | Debian or Ubuntu | Fedora or RHEL | Standalone binary |
| --- | --- | --- | --- |
| `x86_64` | `*_amd64.deb` | `*.x86_64.rpm` | `xlui-linux-amd64` |
| `aarch64` | `*_arm64.deb` | `*.aarch64.rpm` | `xlui-linux-arm64` |

From that directory, run `sha256sum --check --ignore-missing SHA256SUMS`. Once your file reports `OK`, use the command for your download.

| Download | Install |
| --- | --- |
| DEB | `sudo apt install ./xlui_*.deb` |
| RPM | `sudo dnf install ./xlui-*.rpm` |
| Standalone | `sudo install -m 0755 xlui-linux-* /usr/local/bin/xlui` |

To install from source on Linux, use Git and Go 1.25.0 or newer. Use Git credentials with access to the repository.

```sh
git clone https://github.com/advnpzn/xlui.git
cd xlui
CGO_ENABLED=0 GOBIN="$HOME/.local/bin" go install -trimpath ./cmd/xlui
export PATH="$HOME/.local/bin:$PATH"
xlui --help
```

If `~/.local/bin` is not already on your `PATH`, add the export line to your shell profile.

For a Yocto-built image, add the `meta-xlui` layer and include `xlui` in the image. [Layer setup and build instructions](packaging/yocto/meta-xlui/README.md)

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

Press **L** or click the current layout in the footer to cycle through dashboard layouts. Use `xlui --layout split` to override the saved starting layout. The layout applies to all open sessions for that run.

| Layout | What it shows |
| --- | --- |
| `overview` | Host trend charts, domain inventory and a selected-domain summary. This is the default. |
| `inventory` | A full-width domain table and a one-line resource summary. |
| `split` | Domain inventory beside live details and charts for the selected domain and host. Narrow terminals use the inventory view until there is space for both panes. |

xlui uses your terminal’s font. JetBrains Mono works well for the charts, box lines and metric columns.

| Reading | Meaning |
| --- | --- |
| CPU load | 100% is one fully busy vCPU. A four-vCPU guest can reach 400%. Local and SSH sessions need two successful samples to calculate load. |
| Allocated | The amount of memory Xen assigns to the domain. The summary adds the allocations of reporting domains. |
| Guest used | On local and SSH connections, xlui reads total minus available memory in Dom0 and total minus reported free memory from each guest’s XenStore agent. XenAPI uses its RRD allocation and guest-free readings. |
| History | The selected domain shows recent CPU and guest-used memory. The top charts show CPU and allocated memory for this session. Gaps indicate unavailable measurements. |

A dash means the provider has no reading. Guest memory requires a reporting guest agent and permission to read its metrics. XenAPI lists VMs across the pool. CPU and guest memory charts cover the VM series served by the configured endpoint. [XenServer metrics documentation](https://docs.xenserver.com/us/en/xenserver/developer/sdk-guide/using-http.html)

### Create and edit guests

Press **g** or click **Guests**. Select an action, fill in the form, and choose **Preview**. Check the connection, guest identity and proposed settings, then select **Apply**. To disable guest changes across all sessions, start `xlui --read-only`.

| Connection | Create a guest | Edit memory or vCPUs |
| --- | --- | --- |
| Local Dom0 or SSH | **Create guest** saves `/etc/xen/<name>.cfg` and starts a Linux guest using a kernel and self-contained initramfs already on Dom0. For disk-backed guests or custom networking, use **Start from saved definition** with a prepared Xen configuration. | Select a guest or open its saved definition by name. Changes apply the next time the guest is created from the file. |
| XenAPI | Select a halted guest, choose **Create guest**, and give the clone a new name. You can also enter the source UUID. The pool returns a new halted VM. | Select a halted VM. Set startup vCPUs within its configured maximum, or set a fixed memory target within its static limits. |

On a XenAPI connection, select a guest and choose **Guests → Rename guest** to change its pool display name. The preview shows the current name, proposed name and VM UUID. The change takes effect immediately and leaves the guest OS hostname and power state alone.

To boot a halted pool VM, select **Guests → Start guest**. Review the connection, name and UUID, then apply. The pool selects the host and runs its pre-boot checks. xlui verifies that the VM reaches Running. Guest OS and application readiness are separate checks.

To stop a running pool VM, select **Guests → Shut down gracefully**. The action appears when the pool advertises guest shutdown and supplies the host and boot information needed to identify the running instance. Review the connection, guest name and UUID before applying. xlui reports success after it observes the VM halted. If the reply is lost, inspect the VM in the pool before retrying.

On Dom0, the account needs permission to run `xl` and work in `/etc/xen`. The write path uses `sh`, `flock` and GNU coreutils. For saved definition edits, the first external `cp` on Dom0’s PATH must support `--attributes-only --preserve=all`. Preview checks this by copying metadata onto a temporary file. Existing files must have a matching guest name and an explicit guest UUID. Edits support single-line assignments and an explicit decimal `memory` or `vcpus` field. Other fields and comments are preserved. Starting a saved definition uses the host's JSON `xl create -n` output and its configured disks and devices. Use a XenAPI session for a pool-managed host.

xlui rechecks the target before dispatch and reads it back to verify the result. If starting a new Dom0 guest fails after its definition is saved, inspect the file and inventory before using **Start from saved definition**. If a pool connection drops during a write, inspect the VM in the pool before retrying.

### Navigation

Mouse controls are enabled automatically in terminals that support mouse reporting. The buttons at the bottom of the screen also show their keyboard shortcuts.

| Action | Keyboard | Mouse |
| --- | --- | --- |
| Create or edit a guest | `g` | Click **Guests** |
| Manage connections | `s` | Click **Sessions** |
| Switch open sessions | Tab | Click **Next** |
| Open monitoring settings | `,` | Click **Settings** |
| Request an inventory refresh now | `r` | Click **Refresh** |
| Cycle dashboard layouts | `L` | Click the current layout |
| Shorten or lengthen the monitoring interval | `-` / `+` | Click **[-]** or **[+]** in the header |
| Select a domain | ↑ / ↓ | Click its row |
| Cycle domain order | `o` | Click **Sort** |
| Open the inspector | Enter | Double-click a row or click **Inspect** |
| Look up a VM’s resident pool host in a XenAPI inspector | h | Click **Host** |
| Inspect virtual network interfaces for a domain or pool VM | n | Click **Network** |
| Sample vCPU placement in a local or SSH inspector | v | Click **vCPUs** |
| Scroll the list or inspector | ↑ / ↓ or Page Up / Page Down | Scroll the wheel over the list or inspector |
| Return to the list | Esc | Click **Back** |
| Filter by name, ID, UUID or state | `/`, then type | Click **Filter**, then type |
| Apply or cancel a filter edit | Enter / Esc | Click **Apply** or **Cancel** |
| Clear the applied filter | Esc in the list | Click **Clear** |
| Quit | `q` outside the filter editor, or Ctrl+C anywhere | Click **Quit** |

Press `o` to cycle through provider order, name, CPU load, allocated memory and guest-used memory. Resource readings sort highest first, with unavailable readings last. Each connection remembers its order while open. Sorting and filtering use the latest cached observation.

The built-in refresh interval is **2s**. Press `,` from the dashboard or inspector to enter an exact interval such as `750ms` or `5s`. Select **Dashboard layout** to choose Overview, Inventory or Split. Select **Domain order** to choose the sort for the active connection. **Apply** updates that connection’s interval and order, and changes the layout across open sessions. **Apply + save default** also sets the defaults for newly opened connections and future runs. xlui stores the default in `preferences.json` alongside the connection profiles. An explicit `--interval` takes precedence for new sessions in that run.

On a XenAPI connection, inspect a running or paused VM and press `h` to look up its current host label and UUID on the pool coordinator. This read-only snapshot checks for migration during the lookup. Press Esc and `h` to sample again.

In a local or SSH inspector, press `n` to read the selected domain’s virtual interfaces with `xl network-list`. Each reported interface shows its MAC, backend domain and XenStore path, Xenbus state, event channel, and TX/RX ring references. A dash marks an unavailable reading.

On an XAPI connection, the same key lists the selected VM’s configured interfaces, including detached interfaces on halted VMs. Each row shows the device, MAC, attachment state, VIF UUID, and pool network name and UUID. Both providers read on demand and support read-only mode. Press Esc to return.

For a local or SSH connection, inspect a domain and press `v` to sample its vCPU placement, including in read-only mode. Each row shows the physical CPU observed by `xl vcpu-list`, scheduler state, cumulative CPU time, and allowed (hard) and preferred (soft) CPU masks. A vCPU can migrate after the sample. Press Esc to return, then `v` for a fresh sample. This view requires an xl version that reports both affinity masks.

Press `-` to refresh faster or `+` to refresh slower in 100 ms steps, down to 100 ms. The header shows the active session’s interval. Changes take effect in the dashboard and inspector and preserve chart history. Use `xlui --host bench --interval 500ms` to start sessions at a different interval. If collection takes longer than the selected interval, xlui skips missed polls. Charts advance as samples arrive.

If a refresh fails, **Stale** and a timestamp identify the last successful sample. The dashboard keeps that inventory visible and displays the latest error below the header when space allows. Open the inspector to see cumulative CPU time where available and separate charts for CPU load, allocated memory and guest-reported used memory. Each chart retains up to 60 samples. For running or paused XenAPI guests that report a boot time, the inspector shows the UTC start time and uptime at the last successful sample. Inspector charts start when opened. A missing guest usage reading or a missed sample appears as `?`. Scroll down in a short terminal to see the charts.

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

- [Management actions](docs/architecture.md#management-actions) covers implemented guest changes and the design for power controls and deletion.
- [TUI design](docs/tui.md) describes screens, widgets and keyboard interactions.
- [Architecture](docs/architecture.md) explains providers, transport and session handling.
- [Contributing](CONTRIBUTING.md) covers local checks, test fixtures and contributions.
