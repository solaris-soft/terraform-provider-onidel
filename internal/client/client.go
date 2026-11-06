// Package client implements the Onidel VM API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.cloud.onidel.com"

const maxResponseBytes = 1 << 20

// Client is safe for concurrent use. Requests never follow redirects, including
// same-origin redirects, so bearer credentials cannot be forwarded.
type Client struct {
	baseURL      *url.URL
	token        string
	httpClient   *http.Client
	pollInterval time.Duration
}

// New constructs a client. An empty baseURL selects DefaultBaseURL. HTTP is
// supported for local testing; production endpoints should always use HTTPS.
// URL credentials, queries, fragments and ambiguous paths are not permitted.
func New(baseURL, token string) (*Client, error) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || strings.Contains(baseURL, "#") || !validBaseURL(u) {
		// Never echo a URL: it may contain credentials even when it is invalid.
		return nil, errors.New("onidel: invalid base URL; use an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if token == "" {
		return nil, errors.New("onidel: API token is required")
	}
	for _, ch := range token {
		if ch <= ' ' || ch > '~' {
			return nil, errors.New("onidel: API token contains invalid characters")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return &Client{
		baseURL: u,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		pollInterval: 2 * time.Second,
	}, nil
}

func validBaseURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" ||
		u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.Contains(u.String(), "#") || u.RawPath != "" || strings.ContainsAny(u.Path, "\\%") {
		return false
	}
	if strings.Contains(u.Hostname(), ":") && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	p := strings.TrimRight(u.Path, "/")
	return p == "" || (strings.HasPrefix(p, "/") && path.Clean(p) == p)
}

// NewVM contains provisioning settings. Optional image sources are omitted when
// unset; callers must supply exactly one of OS, SnapshotID and ISOID.
type NewVM struct {
	Name               string   `json:"name"`
	PaymentCycle       string   `json:"payment_cycle"`
	InstanceType       string   `json:"instance_type"`
	Location           string   `json:"location"`
	CPU                int64    `json:"cpu"`
	RAM                int64    `json:"ram"`
	Disk               int64    `json:"disk"`
	OS                 *int64   `json:"os,omitempty"`
	SnapshotID         string   `json:"snapshot_id,omitempty"`
	ISOID              string   `json:"iso_id,omitempty"`
	BootMode           string   `json:"boot_mode,omitempty"`
	TeamID             string   `json:"team_id,omitempty"`
	SSHKeys            []string `json:"ssh_keys,omitempty"`
	VPCs               []string `json:"vpcs,omitempty"`
	FirewallGroupID    string   `json:"firewall_group_id,omitempty"`
	IPv6               bool     `json:"ipv6"`
	DisableSSHBlocking bool     `json:"disable_ssh_blocking"`
	StartupScriptID    string   `json:"startup_script_id,omitempty"`
}

// VM contains the API fields used by the provider. Password is sensitive and
// must not be included in diagnostics or logs.
type VM struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Password        string  `json:"password"`
	MainIPv4        string  `json:"main_ipv4"`
	MainIPv6        string  `json:"main_ipv6"`
	Template        string  `json:"template"`
	BootMode        string  `json:"boot_mode"`
	Status          string  `json:"status"`
	VCPU            int64   `json:"vcpu"`
	RAM             int64   `json:"ram"`
	Disk            int64   `json:"disk"`
	FirewallGroupID *string `json:"firewall_group_id"`
}

// UpdateVM supports the rename action only.
type UpdateVM struct {
	Name   string `json:"name"`
	TeamID string `json:"team_id,omitempty"`
}

type statusError struct {
	method string
	code   int
}

func (e *statusError) Error() string {
	// Response bodies, reason phrases and URLs are untrusted and may echo secrets.
	return fmt.Sprintf("onidel: %s VM request failed (HTTP %d)", e.method, e.code)
}

// IsNotFound reports whether err (possibly wrapped) is an API 404 response.
func IsNotFound(err error) bool {
	var e *statusError
	return errors.As(err, &e) && e.code == http.StatusNotFound
}

func validID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, ch := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if ch != '-' {
				return false
			}
		} else if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return false
		}
	}
	return true
}

func (c *Client) endpoint(id, teamID string) *url.URL {
	u := *c.baseURL
	u.Path += "/vm"
	if id != "" {
		u.Path += "/" + id
	}
	if teamID != "" {
		q := url.Values{"team_id": {teamID}}
		u.RawQuery = q.Encode()
	}
	return &u
}

func (c *Client) request(ctx context.Context, method string, u *url.URL, input any) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return nil, "", errors.New("onidel: could not encode VM request")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, "", errors.New("onidel: could not construct VM request")
	}
	if method == http.MethodPost {
		// NewRequest gives bytes.Reader a replay function automatically. Do not
		// offer the transport a way to replay a billable creation request.
		req.GetBody = nil
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		// Transport errors can include URLs, headers or remote-controlled text.
		return nil, "", errors.New("onidel: VM request transport failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &statusError{method: method, code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}
	if err != nil {
		return nil, "", errors.New("onidel: could not read VM response")
	}
	if len(data) > maxResponseBytes {
		return nil, "", errors.New("onidel: VM response exceeds size limit")
	}
	return data, resp.Header.Get("Location"), nil
}

// CreateVM sends exactly one POST. The API specification does not define its
// response schema: we support a direct VM object (including an ID-only object),
// or a Location identifying a VM on the configured API endpoint. No follow-up
// request is made here. If no trustworthy ID is returned, creation may still
// have succeeded: callers must reconcile manually, not automatically retry or
// identify the VM by name.
func (c *Client) CreateVM(ctx context.Context, input NewVM) (*VM, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := c.endpoint("", "")
	data, location, err := c.request(ctx, http.MethodPost, u, input)
	if err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code < 500 {
			return nil, err
		}
		return nil, fmt.Errorf("onidel: creation may have succeeded; reconcile manually before retrying: %w", err)
	}
	// Decode identity independently: optional fields in this undocumented
	// response must not prevent tracking a successfully provisioned resource.
	var identity struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &identity) == nil && identity.ID != "" {
		if !validID(identity.ID) {
			return nil, errors.New("onidel: creation response contains an invalid VM ID; creation may have succeeded; reconcile manually before retrying")
		}
		var vm VM
		if json.Unmarshal(data, &vm) != nil {
			return &VM{ID: identity.ID}, nil
		}
		return &vm, nil
	}
	if id := c.locationID(u, location); id != "" {
		return &VM{ID: id}, nil
	}
	return nil, errors.New("onidel: creation response has no trustworthy VM ID (POST response schema is undocumented); creation may have succeeded; reconcile manually before retrying")
}

func (c *Client) locationID(requestURL *url.URL, location string) string {
	if location == "" {
		return ""
	}
	u, err := url.Parse(location)
	if err != nil || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.Contains(location, "#") || u.RawPath != "" || strings.ContainsAny(u.Path, "\\%") {
		return ""
	}
	// Reject traversal before ResolveReference normalizes it away.
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return ""
		}
	}
	u = requestURL.ResolveReference(u)
	if u.Scheme != c.baseURL.Scheme || !strings.EqualFold(u.Host, c.baseURL.Host) {
		return ""
	}
	prefix := c.baseURL.Path + "/vm/"
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	id := strings.TrimPrefix(u.Path, prefix)
	if !validID(id) {
		return ""
	}
	return id
}

func (c *Client) GetVM(ctx context.Context, id, teamID string) (*VM, error) {
	if !validID(id) {
		return nil, errors.New("onidel: VM ID must be a UUID")
	}
	data, _, err := c.request(ctx, http.MethodGet, c.endpoint(id, teamID), nil)
	if err != nil {
		return nil, err
	}
	var vm VM
	if json.Unmarshal(data, &vm) != nil || !validID(vm.ID) || !strings.EqualFold(vm.ID, id) {
		return nil, errors.New("onidel: invalid VM response or mismatched VM ID")
	}
	return &vm, nil
}

func (c *Client) UpdateVM(ctx context.Context, id string, input UpdateVM) error {
	if !validID(id) {
		return errors.New("onidel: VM ID must be a UUID")
	}
	_, _, err := c.request(ctx, http.MethodPatch, c.endpoint(id, ""), input)
	return err
}

func (c *Client) DeleteVM(ctx context.Context, id string) error {
	if !validID(id) {
		return errors.New("onidel: VM ID must be a UUID")
	}
	_, _, err := c.request(ctx, http.MethodDelete, c.endpoint(id, ""), nil)
	return err
}

// WaitVM polls immediately, then every two seconds while building or restoring.
// Only active is success. Other states fail explicitly rather than polling
// forever; the caller controls the overall deadline through ctx.
func (c *Client) WaitVM(ctx context.Context, id, teamID string) (*VM, error) {
	for {
		vm, err := c.GetVM(ctx, id, teamID)
		if err != nil {
			return nil, err
		}
		switch vm.Status {
		case "active":
			return vm, nil
		case "building", "restoring":
			// Continue below with a cancellable delay.
		case "suspended", "awaiting_payment", "terminating":
			return nil, fmt.Errorf("onidel: VM cannot become ready in state %s", vm.Status)
		default:
			// Do not echo an unknown status: it could contain credentials.
			return nil, errors.New("onidel: VM is not active and has an unsupported provisioning state")
		}
		timer := time.NewTimer(c.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
