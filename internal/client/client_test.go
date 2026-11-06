package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testID    = "c1d45377-b2a9-4407-ab8d-6909c34dfaac"
	otherID   = "53959c6e-73d5-4476-9b19-d78fb8efac2c"
	testToken = "secret-test-token"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c, err := New(s.URL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	c.pollInterval = time.Millisecond
	return c, s
}

func assertSanitized(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range append(secrets, testToken) {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed a credential: %q", err)
		}
	}
}

func TestNew(t *testing.T) {
	for _, endpoint := range []string{"", "https://api.cloud.onidel.com", "http://localhost:8080", "http://127.0.0.1:1234/api/v1/", "https://[::1]:443/"} {
		t.Run("valid_"+endpoint, func(t *testing.T) {
			c, err := New(endpoint, testToken)
			if err != nil {
				t.Fatal(err)
			}
			if endpoint == "" && c.baseURL.String() != DefaultBaseURL {
				t.Fatalf("unexpected default: %s", c.baseURL)
			}
		})
	}
	for _, endpoint := range []string{
		"/relative", "api.example.com", "//example.com", "ftp://example.com", "https:", "https:///vm",
		"https://user:password@example.com", "https://example.com?token=" + testToken,
		"https://example.com?", "https://example.com#" + testToken, "https://example.com/#",
		"https://example.com:bad", "https://example.com:0", "https://example.com:65536", "https://example.com:", "https://[not:ip]",
		"https://example.com/a/../b", "https://example.com/a//b", "https://example.com/%2e%2e", "https://example.com/a\\b",
		"https://example.com/%zz", "https://" + testToken + "\n.example.com",
	} {
		t.Run("invalid_"+endpoint, func(t *testing.T) {
			_, err := New(endpoint, testToken)
			assertSanitized(t, err, "password")
		})
	}
	for _, token := range []string{"", " ", "a b", "secret\r\nInjected: yes", "secret\x00", "secret\x7f", "secreté"} {
		if _, err := New(DefaultBaseURL, token); err == nil {
			t.Errorf("accepted invalid token")
		} else if strings.TrimSpace(token) != "" && strings.Contains(err.Error(), token) {
			t.Errorf("error echoed invalid token")
		}
	}
}

func TestVMRequests(t *testing.T) {
	os := int64(2)
	input := NewVM{
		Name: "vm", PaymentCycle: "monthly", InstanceType: otherID, Location: "Sydney",
		CPU: 2, RAM: 4096, Disk: 40, OS: &os, BootMode: "uefi", TeamID: otherID,
		SSHKeys: []string{otherID}, VPCs: []string{otherID}, FirewallGroupID: otherID,
		IPv6: true, DisableSSHBlocking: true, StartupScriptID: otherID,
	}
	var calls []string
	c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method)
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("unexpected Authorization")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing JSON Accept")
		}
		expectedPath := "/api/v1/vm/" + testID
		if r.Method == http.MethodPost {
			expectedPath = "/api/v1/vm"
		}
		if r.URL.Path != expectedPath {
			t.Errorf("path = %q; want %q", r.URL.Path, expectedPath)
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("missing JSON Content-Type")
			}
		}
		switch r.Method {
		case http.MethodPost:
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			want := map[string]any{
				"name": "vm", "payment_cycle": "monthly", "instance_type": otherID, "location": "Sydney",
				"cpu": float64(2), "ram": float64(4096), "disk": float64(40), "os": float64(2),
				"boot_mode": "uefi", "team_id": otherID, "ssh_keys": []any{otherID}, "vpcs": []any{otherID},
				"firewall_group_id": otherID, "ipv6": true, "disable_ssh_blocking": true, "startup_script_id": otherID,
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("POST JSON = %#v; want %#v", got, want)
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"id":%q,"name":"vm","status":"building"}`, testID)
		case http.MethodGet:
			if r.URL.Query().Get("team_id") != "team +/&?=" || len(r.URL.Query()) != 1 {
				t.Errorf("unsafe query encoding: %q", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"id":%q,"name":"vm","password":"root-secret","main_ipv4":"192.0.2.1","main_ipv6":"2001:db8::1","template":"Ubuntu","boot_mode":"uefi","status":"active","vcpu":2,"ram":4096,"disk":40,"firewall_group_id":%q,"unknown_future_field":true}`, testID, otherID)
		case http.MethodPatch:
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			if !reflect.DeepEqual(got, map[string]any{"name": "renamed", "team_id": otherID}) {
				t.Errorf("PATCH JSON = %#v", got)
			}
			w.WriteHeader(http.StatusAccepted)
		case http.MethodDelete:
			if r.URL.RawQuery != "" || r.ContentLength != 0 {
				t.Error("DELETE must have no query or body")
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})
	var err error
	c, err = New(s.URL+"/api/v1/", testToken)
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateVM(context.Background(), input)
	if err != nil || created.ID != testID || created.Status != "building" {
		t.Fatalf("create = %#v, %v", created, err)
	}
	vm, err := c.GetVM(context.Background(), testID, "team +/&?=")
	if err != nil {
		t.Fatal(err)
	}
	want := &VM{ID: testID, Name: "vm", Password: "root-secret", MainIPv4: "192.0.2.1", MainIPv6: "2001:db8::1", Template: "Ubuntu", BootMode: "uefi", Status: "active", VCPU: 2, RAM: 4096, Disk: 40, FirewallGroupID: ptr(otherID)}
	if !reflect.DeepEqual(vm, want) {
		t.Errorf("GET = %#v; want %#v", vm, want)
	}
	if err := c.UpdateVM(context.Background(), testID, UpdateVM{Name: "renamed", TeamID: otherID}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteVM(context.Background(), testID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"POST", "GET", "PATCH", "DELETE"}) {
		t.Errorf("unexpected calls: %v", calls)
	}
}

func ptr(s string) *string { return &s }

func TestOptionalJSON(t *testing.T) {
	zero := int64(0)
	for _, tc := range []struct {
		name  string
		input NewVM
		key   string
		value any
	}{
		{"os_zero_not_omitted", NewVM{OS: &zero}, "os", float64(0)},
		{"snapshot", NewVM{SnapshotID: otherID}, "snapshot_id", otherID},
		{"iso", NewVM{ISOID: otherID}, "iso_id", otherID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if got[tc.key] != tc.value {
					t.Errorf("%s = %v", tc.key, got[tc.key])
				}
				for _, key := range []string{"os", "snapshot_id", "iso_id", "boot_mode", "team_id", "ssh_keys", "vpcs", "firewall_group_id", "startup_script_id"} {
					if _, ok := got[key]; ok && key != tc.key {
						t.Errorf("unset field %s was sent", key)
					}
				}
				for _, key := range []string{"ipv6", "disable_ssh_blocking"} {
					if got[key] != false {
						t.Errorf("%s should be explicit false", key)
					}
				}
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"id":%q}`, testID)
			})
			if _, err := c.CreateVM(context.Background(), tc.input); err != nil {
				t.Fatal(err)
			}
		})
	}
	data, err := json.Marshal(UpdateVM{Name: "new-name"})
	if err != nil || string(data) != `{"name":"new-name"}` {
		t.Fatalf("rename-only JSON = %s, %v", data, err)
	}
}

func TestCreateResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, location string
		ok                   bool
	}{
		{"direct", `{"id":"` + testID + `","name":"vm"}`, "", true},
		{"id_only", `{"id":"` + testID + `"}`, "", true},
		{"id_with_incompatible_optional_field", `{"id":"` + testID + `","ram":"4096"}`, "", true},
		{"root_location", "", "/vm/" + testID, true},
		{"relative_location", "", "vm/" + testID, true},
		{"absolute_location", "", "SELF/vm/" + testID, true},
		{"unknown_body_location", `{"message":"created"}`, "/vm/" + testID, true},
		{"non_json_location", "created", "/vm/" + testID, true},
		{"body_precedence", `{"id":"` + testID + `"}`, "https://evil.example/vm/" + otherID, true},
		{"missing", "", "", false},
		{"object_no_id", `{"name":"vm"}`, "", false},
		{"null", "null", "", false},
		{"array", `[{"id":"` + testID + `"}]`, "", false},
		{"wrapped", `{"vm":{"id":"` + testID + `"}}`, "", false},
		{"trailing_json", `{"id":"` + testID + `"} {}`, "", false},
		{"malformed", `{"password":"root-secret"`, "", false},
		{"invalid_id", `{"id":"` + testToken + `"}`, "/vm/" + testID, false},
		{"foreign_origin", "", "https://evil.example/vm/" + testID, false},
		{"foreign_scheme", "", "https://SELFHOST/vm/" + testID, false},
		{"network_reference", "", "//evil.example/vm/" + testID, false},
		{"userinfo", "", "http://user:root-secret@SELFHOST/vm/" + testID, false},
		{"query", "", "/vm/" + testID + "?token=" + testToken, false},
		{"empty_query", "", "/vm/" + testID + "?", false},
		{"fragment", "", "/vm/" + testID + "#root-secret", false},
		{"empty_fragment", "", "/vm/" + testID + "#", false},
		{"other_resource", "", "/snapshots/" + testID, false},
		{"extra_segment", "", "/vm/" + testID + "/delete", false},
		{"traversal", "", "/other/../vm/" + testID, false},
		{"encoded_path", "", "/%76m/" + testID, false},
		{"bad_escape", "", "/vm/%zz", false},
		{"not_uuid", "", "/vm/" + testToken, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			var baseURL string
			c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/vm" {
					t.Errorf("unexpected lookup/retry: %s %s", r.Method, r.URL.Path)
				}
				location := strings.ReplaceAll(tc.location, "SELFHOST", strings.TrimPrefix(baseURL, "http://"))
				location = strings.ReplaceAll(location, "SELF", baseURL)
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusCreated)
				io.WriteString(w, tc.body)
			})
			baseURL = s.URL
			vm, err := c.CreateVM(context.Background(), NewVM{Name: "never-look-up-by-name"})
			if tc.ok {
				if err != nil || vm.ID != testID {
					t.Fatalf("create = %#v, %v", vm, err)
				}
			} else {
				assertSanitized(t, err, "root-secret")
				if !strings.Contains(err.Error(), "creation may have succeeded") {
					t.Errorf("missing reconciliation guidance: %v", err)
				}
			}
			if calls.Load() != 1 {
				t.Errorf("made %d requests; want exactly one POST", calls.Load())
			}
		})
	}
}

func TestLocationBasePath(t *testing.T) {
	for _, location := range []string{"/api/v1/vm/" + testID, "vm/" + testID, "/vm/" + testID} {
		c, s := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusCreated)
		})
		var err error
		c, err = New(s.URL+"/api/v1", testToken)
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.CreateVM(context.Background(), NewVM{})
		if (err == nil) != (location != "/vm/"+testID) {
			t.Errorf("Location %q: %v", location, err)
		}
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, status := range []int{400, 401, 402, 403, 404, 409, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"token":%q,"password":"root-secret"}`, testToken)
			})
			operations := []func() error{
				func() error { _, err := c.CreateVM(context.Background(), NewVM{}); return err },
				func() error { _, err := c.GetVM(context.Background(), testID, ""); return err },
				func() error { return c.UpdateVM(context.Background(), testID, UpdateVM{}) },
				func() error { return c.DeleteVM(context.Background(), testID) },
				func() error { _, err := c.WaitVM(context.Background(), testID, ""); return err },
			}
			for _, op := range operations {
				err := op()
				assertSanitized(t, err, "root-secret")
				if !strings.Contains(err.Error(), fmt.Sprint(status)) {
					t.Errorf("missing HTTP status: %v", err)
				}
				if IsNotFound(fmt.Errorf("wrapped: %w", err)) != (status == 404) {
					t.Errorf("incorrect IsNotFound: %v", err)
				}
			}
			if calls.Load() != int32(len(operations)) {
				t.Errorf("unexpected retries: %d calls", calls.Load())
			}
		})
	}
	if IsNotFound(nil) || IsNotFound(errors.New("404")) {
		t.Error("IsNotFound matched a non-API error")
	}
}

func TestRejectRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, sameOrigin := range []bool{true, false} {
			t.Run(fmt.Sprintf("%d_same_origin_%t", status, sameOrigin), func(t *testing.T) {
				var forwarded atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					forwarded.Add(1)
				}))
				defer target.Close()
				var requests atomic.Int32
				c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if r.URL.Path == "/redirected" {
						forwarded.Add(1)
						return
					}
					location := target.URL + "/redirected"
					if sameOrigin {
						location = "/redirected"
					}
					w.Header().Set("Location", location+"?token="+testToken)
					w.WriteHeader(status)
				})
				_, err := c.GetVM(context.Background(), testID, "")
				assertSanitized(t, err)
				_, err = c.CreateVM(context.Background(), NewVM{})
				assertSanitized(t, err)
				if forwarded.Load() != 0 || requests.Load() != 2 {
					t.Errorf("redirect was followed: forwarded=%d, requests=%d", forwarded.Load(), requests.Load())
				}
			})
		}
	}
}

func TestGetInvalidResponses(t *testing.T) {
	for _, body := range []string{"", "null", "[]", `{}`, `{"id":"` + otherID + `"}`, `{"id":"` + testID + `","vcpu":"` + testToken + `"}`, `{"password":"root-secret"`, `{"id":"` + testID + `"} {}`} {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := c.GetVM(context.Background(), testID, "")
		assertSanitized(t, err, "root-secret")
	}
	for _, field := range []string{"", `,"firewall_group_id":null`} {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RawQuery != "" {
				t.Error("unset team_id should be omitted")
			}
			fmt.Fprintf(w, `{"id":%q%s}`, testID, field)
		})
		vm, err := c.GetVM(context.Background(), testID, "")
		if err != nil || vm.FirewallGroupID != nil {
			t.Fatalf("nullable firewall group = %#v, %v", vm, err)
		}
	}
}

func TestInvalidIDsNeverRequested(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request made for invalid ID") })
	for _, id := range []string{"", "..", "../vm/" + testID, testID + "?token=x", testID + "/", testToken, strings.ReplaceAll(testID, "-", ""), strings.ReplaceAll(testID, "-", "_"), strings.Replace(testID, "c", "z", 1)} {
		_, err := c.GetVM(context.Background(), id, "")
		assertSanitized(t, err)
		assertSanitized(t, c.UpdateVM(context.Background(), id, UpdateVM{}))
		assertSanitized(t, c.DeleteVM(context.Background(), id))
	}
}

func TestResponseSizeLimit(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
	})
	_, err := c.GetVM(context.Background(), testID, "")
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected response size error, got %v", err)
	}
}

func TestWaitVM(t *testing.T) {
	var calls atomic.Int32
	states := []string{"building", "restoring", "active"}
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("team_id") != otherID {
			t.Error("poll omitted team_id")
		}
		i := calls.Add(1) - 1
		if int(i) >= len(states) {
			t.Error("polled after active")
			w.WriteHeader(500)
			return
		}
		fmt.Fprintf(w, `{"id":%q,"status":%q}`, testID, states[i])
	})
	vm, err := c.WaitVM(context.Background(), testID, otherID)
	if err != nil || vm.Status != "active" || calls.Load() != 3 {
		t.Fatalf("wait = %#v, %v, calls=%d", vm, err, calls.Load())
	}
	for _, state := range []string{"suspended", "awaiting_payment", "terminating", "failed", "error", "migrating", "taking_snaphot", "", testToken} {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"id":%q,"status":%q}`, testID, state)
		})
		_, err := c.WaitVM(context.Background(), testID, "")
		assertSanitized(t, err)
	}
}

func TestCancellation(t *testing.T) {
	t.Run("before_request", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request after cancellation") })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, createErr := c.CreateVM(ctx, NewVM{})
		_, getErr := c.GetVM(ctx, testID, "")
		_, waitErr := c.WaitVM(ctx, testID, "")
		for _, err := range []error{createErr, getErr, waitErr, c.UpdateVM(ctx, testID, UpdateVM{}), c.DeleteVM(ctx, testID)} {
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		}
	})
	for _, flushHeaders := range []bool{false, true} {
		t.Run(fmt.Sprintf("in_flight_flush_%t", flushHeaders), func(t *testing.T) {
			started := make(chan struct{})
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if flushHeaders {
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := c.GetVM(ctx, testID, ""); result <- err }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("expected context.Canceled, got %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request ignored cancellation")
			}
		})
	}
	t.Run("poll_delay", func(t *testing.T) {
		var calls atomic.Int32
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			fmt.Fprintf(w, `{"id":%q,"status":"building"}`, testID)
		})
		c.pollInterval = time.Hour
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_, err := c.WaitVM(ctx, testID, "")
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Errorf("wait did not stop during delay: err=%v, calls=%d", err, calls.Load())
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTruncatedResponse(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n%s", testToken)
		buf.Flush()
	})
	_, err := c.GetVM(context.Background(), testID, "")
	assertSanitized(t, err)
	_, err = c.CreateVM(context.Background(), NewVM{})
	assertSanitized(t, err)
	if !strings.Contains(err.Error(), "creation may have succeeded") {
		t.Errorf("missing ambiguous creation warning: %v", err)
	}
}

func TestCreateCancellationAfterDispatch(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.CreateVM(ctx, NewVM{}); result <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("POST did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "creation may have succeeded") {
			t.Errorf("expected cancellation and reconciliation guidance, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST ignored cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("POST retried after cancellation: %d calls", calls.Load())
	}
}

func TestCreateRequestNotReplayable(t *testing.T) {
	c, err := New(DefaultBaseURL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.GetBody != nil || r.ContentLength == 0 || r.Header.Get("Idempotency-Key") != "" {
			t.Error("creation request permits automatic replay")
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"id":"` + testID + `"}`)),
		}, nil
	})
	if _, err := c.CreateVM(context.Background(), NewVM{}); err != nil {
		t.Fatal(err)
	}
}

func TestTransportErrorsSanitized(t *testing.T) {
	c, err := New(DefaultBaseURL, testToken)
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport echoed " + testToken)
	})
	_, err = c.GetVM(context.Background(), testID, "root-secret")
	assertSanitized(t, err, "root-secret")
}
