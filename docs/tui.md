# TUI components and the session interface

The terminal client has three responsibilities. Widgets handle reusable terminal interactions. Screens give those interactions meaning for a Xen operator. A host session supplies observations and executes typed requests. These are internal packages in the xlui Go module. The headless interface and a future managed API can use the same session with no terminal dependencies.

This is the design for the interactive client. The current executable supports the headless inventory command.

## Where code belongs

| Planned location | What belongs there | Example |
| --- | --- | --- |
| `internal/tui/app.go` | tview application, focus, screen stack, global keys and scheduling redraws | Esc dismisses the active dialog and restores focus. |
| `internal/tui/domains.go`, `inspector.go`, `settings.go` | Xen-specific screen composition, screen-local state and mapping between observations and display values | The domains screen formats allocated MiB, maps a selected row to a domain reference and opens the inspector. |
| `internal/tui/widgets/` | Reusable interactions with concrete inputs and callbacks | A selectable table takes column definitions and rows with stable keys. A time-series plot takes timestamped points, gaps and a unit label. |
| `internal/tui/ports.go` | Small Go interfaces required by the screens | An inventory subscriber can draw the table. An inspector additionally needs a detail reader. Only an action flow needs write methods. |
| `internal/session/` | A context-bound application service with sampling, capabilities, cached observations, previewed actions and operation outcomes | Both local and SSH backends can satisfy the same screens. A replay session can feed a table without implementing writes. |

Use tview's existing table, form, text and focus primitives directly where they already fit. Extract a widget when xlui needs shared behavior, such as retaining selection across sorted table updates or drawing a gap in a time series. Keep Xen names, tool invocations, per-domain permissions and connection contexts out of `widgets`. Start with concrete Go types. Add an interface at the consuming boundary when another implementation or a test actually needs it.

## Application interface

The executable constructs the selected provider and session, then injects the narrow interfaces into the TUI. These proposed signatures show the boundary. The session and Xen value types contain ordinary Go data and can be used from a CLI, API handler or test.

```go
type Inventory interface {
    WatchInventory(ctx context.Context) <-chan xen.Snapshot
}

type DetailReader interface {
    Inspect(ctx context.Context, target xen.DomainRef) (xen.DomainDetail, error)
}

type ShutdownActions interface {
    PreviewShutdown(ctx context.Context, target xen.DomainRef) (session.ShutdownPlan, error)
    ConfirmShutdown(ctx context.Context, plan session.PlanID) (session.OperationID, error)
    Operation(ctx context.Context, id session.OperationID) (session.Operation, error)
}
```

The subscriber receives the latest snapshot immediately when available. Each snapshot identifies the host, sample time, freshness and collection status, including an initial failure with no data. The session may replace an unread snapshot with a newer one. Cancelling the subscription releases its resources and closes its channel. Operation results have IDs and remain queryable for the lifetime of the session even if an intermediate screen update is skipped. The existing `list` command deliberately streams the host's original output. An interactive screen requests typed observations through the session.

## One domain from the keyboard to the host

| Operator input or event | Component behavior | Host or session effect |
| --- | --- | --- |
| Press `/`, type a filter, sort, or resize | The domains screen filters and projects its current snapshot. The table preserves the selected key if still visible. If it disappears, clear the selection and require a new one before enabling actions. | Zero host requests. A redraw does not trigger a refresh. |
| Press Enter on a domain | The screen resolves its row key to the host-scoped instance reference. The inspector requests detail and displays a loading state. | A background request uses the session cache or queries the provider. A late result is applied only if it matches the active host, instance and request. |
| Receive a monitoring sample | Every visible chart projects the same bounded history. A missing value creates a gap. The compact view can use text values at 80×24 or in monochrome. | One sampler serves the active session. Adding a widget never starts a second collector. |
| Open a shutdown dialog and confirm | The screen displays the target and consequence supplied by the preview. The confirmation callback passes back its plan ID. | The session checks the bound target, capability, freshness and current instance before dispatch. The screen follows the operation ID until a verified outcome or an explicit unknown state. |
| Open `:settings` | Theme and columns repaint locally. A sampling interval updates the session timer. The settings screen owns unsaved edits and reports a save failure. | Preferences are saved in the operator's configuration. Changing a guest's resources is a host action with its own preview and authority check. |

## State and concurrency

- **Widgets** own cursor position, scrolling and focus. **Screens** own filter text, sort order, the selected domain reference and navigation state. **The session** owns host identity, capabilities, observations and pending operations. Selection and action targets use host-scoped instance evidence. Names, row indices and domids alone are insufficient.
- Keep the tview event loop free of collection, SSH and filesystem I/O. Background work publishes immutable data. Schedule UI changes with tview's event-loop mechanism and coalesce pending redraws. A slow terminal can skip an intermediate frame. Render host-supplied text as literal content.
- Scope a detail request to the active inspector. Switching host cancels the previous subscription and detail reads, drops old frames and invalidates unconfirmed action plans. After a write was dispatched, leaving the screen or losing the connection cannot prove it failed. Preserve the operation's last known status and reconcile before reporting success or retrying.

## Tests at each boundary

Test the reusable table with arbitrary keys, row reorder, a disappearing selection and a narrow terminal. Test screens with an in-memory inventory and a counting detail reader. Typing a filter must make zero provider calls. A detail reply from the previous host must never update the new inspector. Use a simulated tcell screen for focus, key bindings and layout. Test identity rechecks and ambiguous write outcomes at the session boundary, where they also protect headless callers.
