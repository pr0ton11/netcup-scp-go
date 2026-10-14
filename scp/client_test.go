package scp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestActionPaths guards the custom method paths (":action" suffix). They must reach the
// server as written in the spec, not rewritten to "/action".
func TestActionPaths(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.EscapedPath())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	calls := []func() (*http.Response, error){
		func() (*http.Response, error) {
			return c.PostApiV1ServersServerIdInterfacesMacFirewallReapply(ctx, 1, "aa:bb:cc:dd:ee:ff")
		},
		func() (*http.Response, error) {
			return c.PostApiV1ServersServerIdInterfacesMacFirewallRestoreCopiedPolicies(ctx, 1, "aa:bb:cc:dd:ee:ff")
		},
		func() (*http.Response, error) { return c.PostApiV1ServersServerIdDisksDiskNameFormat(ctx, 1, "vda") },
		func() (*http.Response, error) {
			return c.PostApiV1ServersServerIdSnapshotsDryrun(ctx, 1, PostApiV1ServersServerIdSnapshotsDryrunJSONRequestBody{})
		},
		func() (*http.Response, error) { return c.PutApiV1TasksUuidCancel(ctx, "u") },
	}
	for _, call := range calls {
		resp, err := call()
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	want := []string{
		"POST /api/v1/servers/1/interfaces/aa:bb:cc:dd:ee:ff/firewall:reapply",
		"POST /api/v1/servers/1/interfaces/aa:bb:cc:dd:ee:ff/firewall:restore-copied-policies",
		"POST /api/v1/servers/1/disks/vda:format",
		"POST /api/v1/servers/1/snapshots:dryrun",
		"PUT /api/v1/tasks/u:cancel",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestFirewallPolicyNulls checks that null values from the API (nullable in the spec) decode.
func TestFirewallPolicyNulls(t *testing.T) {
	body := `{"id":7,"name":"crowdsec","description":null,"rules":[
		{"direction":"INGRESS","protocol":"TCP","action":"DROP","sources":["192.0.2.1","2001:db8::/32"],
		 "destinationPorts":null,"description":null}]}`
	var p FirewallPolicy
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if p.Description != nil || p.Rules == nil || len(*p.Rules) != 1 {
		t.Fatalf("unexpected policy: %+v", p)
	}
	r := (*p.Rules)[0]
	if r.Action != "DROP" || r.Sources == nil || len(*r.Sources) != 2 || r.DestinationPorts != nil {
		t.Fatalf("unexpected rule: %+v", r)
	}
}
