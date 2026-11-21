package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"

	"github.com/advnpzn/xlui/internal/config"
	"github.com/advnpzn/xlui/internal/xen/xapi"
)

func listXAPI(ctx context.Context, endpoint, username, caFile string, stdout io.Writer) error {
	password := os.Getenv("XLUI_XAPI_PASSWORD")
	if password == "" {
		return errors.New("set XLUI_XAPI_PASSWORD for XAPI authentication")
	}
	client, err := xapiClient(caFile)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	vms, err := xapi.List(ctx, endpoint, username, password, client)
	if err != nil {
		return fmt.Errorf("XAPI inventory: %w", err)
	}
	return writeXAPIInventory(stdout, vms)
}

func writeXAPIInventory(stdout io.Writer, vms []xapi.VM) error {
	sort.Slice(vms, func(i, j int) bool {
		if vms[i].Name == vms[j].Name {
			return vms[i].UUID < vms[j].UUID
		}
		return vms[i].Name < vms[j].Name
	})
	table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(table, "UUID\tPOWER STATE\tCONTROL DOMAIN\tNAME"); err != nil {
		return err
	}
	for _, vm := range vms {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%t\t%s\n", terminalCell(vm.UUID), terminalCell(vm.PowerState), vm.ControlDomain, terminalCell(vm.Name)); err != nil {
			return err
		}
	}
	return table.Flush()
}

// Keep host-provided control characters visible without treating them as terminal input.
func terminalCell(s string) string {
	quoted := strconv.Quote(s)
	return quoted[1 : len(quoted)-1]
}

func xapiClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := config.ReadFile(caFile, 4<<20)
		if err != nil {
			return nil, fmt.Errorf("read XAPI CA file: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			// An embedded client can have only the explicitly supplied CA bundle.
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("XAPI CA file contains no valid certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport}, nil
}
