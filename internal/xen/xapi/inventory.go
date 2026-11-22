// Package xapi reads inventory from a pool coordinator's XenAPI JSON-RPC endpoint.
package xapi

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// VM describes a guest or control domain visible to the authenticated account.
type VM struct {
	UUID          string
	Name          string
	PowerState    string
	ControlDomain bool
}

type bootSettings struct {
	VCPUMax    json.Number `json:"VCPUs_max"`
	Startup    json.Number `json:"VCPUs_at_startup"`
	StaticMin  json.Number `json:"memory_static_min"`
	StaticMax  json.Number `json:"memory_static_max"`
	DynamicMin json.Number `json:"memory_dynamic_min"`
	DynamicMax json.Number `json:"memory_dynamic_max"`
}

type vmRecord struct {
	bootSettings
	VBDs          []string    `json:"VBDs"`
	VIFs          []string    `json:"VIFs"`
	Allowed       []string    `json:"allowed_operations"`
	Resident      string      `json:"resident_on"`
	DomainID      json.Number `json:"domid"`
	Metrics       string      `json:"metrics"`
	UUID          string      `json:"uuid"`
	Name          *string     `json:"name_label"`
	PowerState    string      `json:"power_state"`
	ControlDomain *bool       `json:"is_control_domain"`
	Template      *bool       `json:"is_a_template"`
	Snapshot      *bool       `json:"is_a_snapshot"`
}

// List retrieves pool-wide VM records in one call and closes its session.
// Pass a client with a custom TLS transport to trust a private pool CA.
func List(ctx context.Context, endpoint, username, password string, client *http.Client) ([]VM, error) {
	m, err := NewMonitor(endpoint, username, password, client)
	if err != nil {
		return nil, err
	}
	records, err := m.records(ctx)
	if err = errors.Join(err, m.logout()); err != nil {
		return nil, err
	}
	vms := make([]VM, 0, len(records))
	for _, r := range records {
		vms = append(vms, VM{r.UUID, *r.Name, r.PowerState, *r.ControlDomain})
	}
	// A complete response can race cancellation, including during logout.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return vms, nil
}

type Monitor struct {
	mu                          sync.Mutex
	writeMu                     sync.Mutex
	epoch                       uint64
	rpc, powerRPC               *rpcClient
	username, password, session string
	loginErr                    error
}

func NewMonitor(endpoint, username, password string, client *http.Client) (*Monitor, error) {
	address, err := ParseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	// A guest power operation can outlast the inventory RPC deadline.
	// Both clients share the verified transport and honor a shorter caller timeout.
	newRPC := func(maxWait time.Duration) *rpcClient {
		timeout := client.Timeout
		if timeout <= 0 || timeout > maxWait {
			timeout = maxWait
		}
		return &rpcClient{endpoint: address.String(), http: &http.Client{
			Transport: client.Transport,
			Timeout:   timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}}
	}
	return &Monitor{rpc: newRPC(30 * time.Second), powerRPC: newRPC(2 * time.Minute), username: username, password: password}, nil
}

// ParseEndpoint checks the authority and fixes the path used for JSON-RPC.
func ParseEndpoint(endpoint string) (*url.URL, error) {
	address, err := url.Parse(endpoint)
	if err != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil ||
		address.RawQuery != "" || address.ForceQuery || strings.Contains(endpoint, "#") {
		return nil, errors.New("XAPI endpoint must be an HTTPS address without userinfo, query or fragment")
	}
	switch address.EscapedPath() {
	case "", "/", "/jsonrpc":
		address.Path = "/jsonrpc"
	default:
		return nil, errors.New("XAPI endpoint path must be /jsonrpc")
	}
	return address, nil
}

const sessionLimit = 64 << 10
const inventoryLimit = 32 << 20

var errInvalidSession = errors.New("SESSION_INVALID")
var errDenied = errors.New("XAPI authentication or authorization denied")

func (m *Monitor) records(ctx context.Context) (map[string]vmRecord, error) {
	if m.loginErr != nil {
		return nil, m.loginErr
	}
	if m.session == "" {
		var token string
		err := m.rpc.call(ctx, "session.login_with_password", []string{m.username, m.password, "1.0", "xlui"}, &token, sessionLimit)
		if err != nil {
			if errors.Is(err, errDenied) {
				m.loginErr = err
			}
			return nil, err
		}
		if token == "" || token == "OpaqueRef:NULL" {
			return nil, errors.New("XAPI login returned an empty session reference")
		}
		m.session = token
		m.epoch++
	}
	var records map[string]vmRecord
	err := m.rpc.call(ctx, "VM.get_all_records", []string{m.session}, &records, inventoryLimit)
	if err != nil {
		return nil, err
	}
	if records == nil {
		return nil, errors.New("XAPI returned a null VM inventory")
	}
	seen := make(map[string]bool, len(records))
	for ref, r := range records {
		if r.Template == nil || r.Snapshot == nil {
			return nil, errors.New("XAPI VM record lacks template or snapshot status")
		}
		if *r.Template || *r.Snapshot {
			delete(records, ref)
			continue
		}
		if r.UUID == "" || r.Name == nil || r.PowerState == "" || r.ControlDomain == nil {
			return nil, errors.New("XAPI VM record lacks an inventory field")
		}
		uuid := strings.ToLower(r.UUID)
		if seen[uuid] {
			return nil, errors.New("XAPI inventory has a duplicate VM identity")
		}
		seen[uuid] = true
	}
	return records, nil
}

func (m *Monitor) logout() error {
	if m.session == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.rpc.call(ctx, "session.logout", []string{m.session}, nil, sessionLimit)
	m.session = ""
	return err
}

func (m *Monitor) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.logout()
	m.rpc.http.CloseIdleConnections()
}

// Each client issues its RPCs sequentially.
type rpcClient struct {
	endpoint string
	http     *http.Client
	nextID   int
}

func (c *rpcClient) call(ctx context.Context, method string, params any, result any, limit int64) error {
	c.nextID++
	body, err := json.Marshal(struct {
		Version string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", c.nextID, method, params})
	if err != nil {
		return fmt.Errorf("%s: cannot encode request", method)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: invalid endpoint", method)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
		var unknownCA x509.UnknownAuthorityError
		var badName x509.HostnameError
		var badCert x509.CertificateInvalidError
		if errors.As(err, &unknownCA) || errors.As(err, &badName) || errors.As(err, &badCert) {
			return fmt.Errorf("%s: TLS certificate verification failed", method)
		}
		return fmt.Errorf("%s: failed to reach XAPI endpoint", method)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s: HTTP %d: %w", method, resp.StatusCode, errDenied)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: XAPI returned HTTP %d", method, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
		return fmt.Errorf("%s: failed to read XAPI response", method)
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("%s: XAPI response exceeds %d bytes", method, limit)
	}
	var response struct {
		Version string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Version != "2.0" || response.ID != c.nextID ||
		(len(response.Result) == 0) == (len(response.Error) == 0) {
		return fmt.Errorf("%s: invalid JSON-RPC response", method)
	}
	if len(response.Error) != 0 {
		var failure struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(response.Error, &failure) != nil || failure.Code == nil {
			return fmt.Errorf("%s: invalid JSON-RPC error", method)
		}
		// Error data and unrecognized messages may echo credentials or a session reference.
		switch failure.Message {
		case "HOST_IS_SLAVE":
			return fmt.Errorf("%s: HOST_IS_SLAVE (connect to the pool coordinator)", method)
		case "SESSION_INVALID":
			return fmt.Errorf("%s: %w", method, errInvalidSession)
		case "SESSION_AUTHENTICATION_FAILED", "SESSION_AUTHORIZATION_FAILED":
			return fmt.Errorf("%s: %s: %w", method, failure.Message, errDenied)
		case "RBAC_PERMISSION_DENIED", "MESSAGE_METHOD_UNKNOWN", "MESSAGE_PARAMETER_COUNT_MISMATCH":
			return fmt.Errorf("%s: %s", method, failure.Message)
		default:
			return fmt.Errorf("%s: XAPI error %d", method, *failure.Code)
		}
	}
	if result != nil && json.Unmarshal(response.Result, result) != nil {
		return fmt.Errorf("%s: invalid XAPI result", method)
	}
	return nil
}
