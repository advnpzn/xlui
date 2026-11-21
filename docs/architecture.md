# Architecture and source layout

xlui has one Go module and one operator executable. A Linux workstation connected to an Arm board over SSH can use the same views and Xen parsers as a local session on that board. The workstation runs its own architecture's binary. The board supplies its installed Xen tools and an authorized account.

## Current code

`cmd/xlui` parses the command line and constructs a runner. Its local and remote `list` paths both call `xl.StreamInventory` in `internal/xen/xl`. That function selects `xl list -v` and streams its output. The runner in `internal/transport` handles local process execution or OpenSSH. CLI tests exercise both paths. CI tests and vets the module and checks static Linux builds for amd64 and arm64.

## Package map

The three packages above exist today. The remaining entries define where the corresponding features belong when implemented. See the [TUI component and session design](tui.md) for screen composition, Go interfaces and ownership of state.

| Path | Responsibility |
| --- | --- |
| `cmd/xlui/` | Parse CLI flags, handle process signals and exit codes, select a context and wire concrete dependencies. Preserve the existing headless `list` command and its streamed output. |
| `internal/xen/` | Define the shared vocabulary for host and domain identities, snapshots, observations, capabilities and typed actions. Keep it independent of terminal widgets, process execution and serialization formats. |
| `internal/xen/xl/` | Implement the plain-Xen provider. Choose Xen tool invocations, recognize supported output, parse domain state and translate typed actions into validated arguments. Keep anonymized tool output under this package's `testdata/`. |
| `internal/transport/` | Execute a command on local Dom0 or through OpenSSH. Own cancellation, remote-shell argument encoding, connection reuse, stderr and exit status. Give an interactive console an explicit PTY path. |
| `internal/session/` | Own one active host session, its refresh schedule, published snapshots, bounded metric histories and pending actions. Define small interfaces here for the provider operations this package consumes. |
| `internal/tui/` | Compose Xen screens, handle navigation, and bind typed session data to the terminal. Keep reusable presentation behavior in `internal/tui/widgets/`. Use tview over tcell, with asciigraph planned for expanded plots. |
| `internal/config/` | Read and save operator preferences and named connection contexts. A terminal theme or refresh interval belongs here. A guest's boot configuration belongs to the host provider. |

`cmd/xlui` is the composition root. The TUI consumes a session. The session consumes provider interfaces that use `internal/xen` types. The plain-Xen provider accepts a runner with the operations it needs. The executable supplies its local or SSH implementation. This keeps a parser test independent of SSH and lets a screen test run with an in-memory session. There is no shared `utils` package or public Go SDK in this design.

## Two paths through the code

**Inspecting a domain.** The session schedules one bulk inventory collection. The provider parses a snapshot and the session publishes it to the UI. Typing `/`, sorting rows or repainting a chart reads the existing snapshot and issues zero host commands. Opening the inspector can request missing detail asynchronously for the selected guest. Keep at most one in-flight request for the same detail. Discard results that arrive after the user switches host or guest. Compute CPU usage from consecutive valid samples of the same instance. A first sample, a reset counter or a gap in collection displays as unavailable. On disconnect, show the age of the last successful observation.

The session owns the mutable sample history. Publish immutable snapshots and coalesce redraws so a slow serial terminal can skip an intermediate frame without blocking collection. Marshal widget changes onto the tview event loop. Changing a theme through `:settings` triggers a local repaint. Changing the polling interval updates the session timer. Both changes can be saved in the operator's configuration.

**Requesting a graceful shutdown.** The UI submits an action for the selected host and observed domain instance. The session checks the owning provider's capability, previews the target, and rechecks identity and state after confirmation. A domid can be reused. Bind the request to the host, UUID and available instance evidence. If a replacement guest cannot be distinguished, stop the action. The provider executes the typed request through the selected transport. The session then observes whether the guest shut down. A lost reply leaves the outcome unknown until reconciled. Switching contexts invalidates pending confirmations. In direct mode the host account supplies authority.

## Extension points

| Addition | Placement and contract |
| --- | --- |
| Offline replay | Add `internal/capture/` for a versioned capture format and read-only replay provider. It feeds the same views and supplies sample timestamps. |
| Managed HTTP API | Add `internal/api/` when implementing service mode. Its handlers authenticate and authorize the caller and use the same session operations. Agentless SSH remains a complete deployment choice on plain Xen. A configured API connection must fail closed. |
| Another management stack | Add a sibling provider under `internal/xen/` for an owning stack such as XAPI or libvirt. Advertise supported reads and actions per target. An XAPI-managed host must never fall back to direct xl writes. |
| Releases and image integration | Put release automation in `.github/workflows/` and binary target and DEB/RPM definitions in `.goreleaser.yaml`. Build one static Go binary per target. DEB and RPM wrap that same binary. Keep the OpenEmbedded layer in `packaging/yocto/meta-xlui/`, with a pinned-source recipe for the image build. Architecture and distro differences belong in build inputs, fixtures or packaging. |

For an automotive bench, an edge board and a homelab server, a missing helper or a restricted account changes the capabilities of a context. It does not justify a separate source tree. Move code behind a new provider only when the management authority or operation semantics change.
