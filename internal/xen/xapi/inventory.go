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
	"time"
)

// VM describes a guest or control domain visible to the authenticated account.
type VM struct {
	UUID          string
	Name          string
	PowerState    string
	ControlDomain bool
}

type vmRecord struct {
	UUID          string  `json:"uuid"`
	Name          *string `json:"name_label"`
	PowerState    string  `json:"power_state"`
	ControlDomain *bool   `json:"is_control_domain"`
	Template      *bool   `json:"is_a_template"`
	Snapshot      *bool   `json:"is_a_snapshot"`
}

// List retrieves pool-wide VM records in one call and closes its session.
// Pass a client with a custom TLS transport to trust a private pool CA.
func List(ctx context.Context, endpoint, username, password string, client *http.Client) ([]VM, error) {
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
	if client == nil {
		client = http.DefaultClient
	}
	timeout := client.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	rpc := rpcClient{endpoint: address.String(), http: &http.Client{
		Transport: client.Transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}

	var session string
	const sessionLimit = 64 << 10
	if err := rpc.call(ctx, "session.login_with_password", []string{username, password, "1.0", "xlui"}, &session, sessionLimit); err != nil {
		return nil, err
	}
	if session == "" || session == "OpaqueRef:NULL" {
		return nil, errors.New("XAPI login returned an empty session reference")
	}

	var records map[string]vmRecord
	params := []string{session}
	const inventoryLimit = 32 << 20
	inventoryErr := rpc.call(ctx, "VM.get_all_records", params, &records, inventoryLimit)
	// Try to release the server-side session even after the original deadline.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	logoutErr := rpc.call(cleanup, "session.logout", params, nil, sessionLimit)
	if err := errors.Join(inventoryErr, logoutErr); err != nil {
		return nil, err
	}
	if records == nil {
		return nil, errors.New("XAPI returned a null VM inventory")
	}
	vms := make([]VM, 0, len(records))
	for _, r := range records {
		if r.Template == nil || r.Snapshot == nil {
			return nil, errors.New("XAPI VM record lacks template or snapshot status")
		}
		if *r.Template || *r.Snapshot {
			continue
		}
		if r.UUID == "" || r.Name == nil || r.PowerState == "" || r.ControlDomain == nil {
			return nil, errors.New("XAPI VM record lacks an inventory field")
		}
		vms = append(vms, VM{UUID: r.UUID, Name: *r.Name, PowerState: r.PowerState, ControlDomain: *r.ControlDomain})
	}
	return vms, nil
}

// Each List invocation owns one sequential RPC client.
type rpcClient struct {
	endpoint string
	http     *http.Client
	nextID   int
}

func (c *rpcClient) call(ctx context.Context, method string, params []string, result any, limit int64) error {
	c.nextID++
	body, err := json.Marshal(struct {
		Version string   `json:"jsonrpc"`
		ID      int      `json:"id"`
		Method  string   `json:"method"`
		Params  []string `json:"params"`
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
		case "SESSION_AUTHENTICATION_FAILED", "SESSION_AUTHORIZATION_FAILED", "SESSION_INVALID",
			"RBAC_PERMISSION_DENIED", "MESSAGE_METHOD_UNKNOWN", "MESSAGE_PARAMETER_COUNT_MISMATCH":
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
