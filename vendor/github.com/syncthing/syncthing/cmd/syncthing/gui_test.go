// Copyright (C) 2016 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/d4l3k/messagediff"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/sync"
	"github.com/thejerf/suture"
)

func TestCSRFToken(t *testing.T) {
	t1 := newCsrfToken()
	t2 := newCsrfToken()

	t3 := newCsrfToken()
	if !validCsrfToken(t3) {
		t.Fatal("t3 should be valid")
	}

	for i := 0; i < 250; i++ {
		if i%5 == 0 {
			// t1 and t2 should remain valid by virtue of us checking them now
			// and then.
			if !validCsrfToken(t1) {
				t.Fatal("t1 should be valid at iteration", i)
			}
			if !validCsrfToken(t2) {
				t.Fatal("t2 should be valid at iteration", i)
			}
		}

		// The newly generated token is always valid
		t4 := newCsrfToken()
		if !validCsrfToken(t4) {
			t.Fatal("t4 should be valid at iteration", i)
		}
	}

	if validCsrfToken(t3) {
		t.Fatal("t3 should have expired by now")
	}
}

func TestStopAfterBrokenConfig(t *testing.T) {
	cfg := config.Configuration{
		GUI: config.GUIConfiguration{
			RawAddress: "127.0.0.1:0",
			RawUseTLS:  false,
		},
	}
	w := config.Wrap("/dev/null", cfg)

	srv := newAPIService(protocol.LocalDeviceID, w, "../../test/h1/https-cert.pem", "../../test/h1/https-key.pem", "", nil, nil, nil, nil, nil, nil, nil, nil)
	srv.started = make(chan string)

	sup := suture.NewSimple("test")
	sup.Add(srv)
	sup.ServeBackground()

	<-srv.started

	// Service is now running, listening on a random port on localhost. Now we
	// request a config change to a completely invalid listen address. The
	// commit will fail and the service will be in a broken state.

	newCfg := config.Configuration{
		GUI: config.GUIConfiguration{
			RawAddress: "totally not a valid address",
			RawUseTLS:  false,
		},
	}
	if err := srv.VerifyConfiguration(cfg, newCfg); err == nil {
		t.Fatal("Verify config should have failed")
	}

	// Nonetheless, it should be fine to Stop() it without panic.

	sup.Stop()
}

func TestAssetsDir(t *testing.T) {
	// For any given request to $FILE, we should return the first found of
	//  - assetsdir/$THEME/$FILE
	//  - compiled in asset $THEME/$FILE
	//  - assetsdir/default/$FILE
	//  - compiled in asset default/$FILE

	// The asset map contains compressed assets, so create a couple of gzip compressed assets here.
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	gw.Write([]byte("default"))
	gw.Close()
	def := buf.Bytes()

	buf = new(bytes.Buffer)
	gw = gzip.NewWriter(buf)
	gw.Write([]byte("foo"))
	gw.Close()
	foo := buf.Bytes()

	e := &staticsServer{
		theme:    "foo",
		mut:      sync.NewRWMutex(),
		assetDir: "testdata",
		assets: map[string][]byte{
			"foo/a":     foo, // overridden in foo/a
			"foo/b":     foo,
			"default/a": def, // overridden in default/a (but foo/a takes precedence)
			"default/b": def, // overridden in default/b (but foo/b takes precedence)
			"default/c": def,
		},
	}

	s := httptest.NewServer(e)
	defer s.Close()

	// assetsdir/foo/a exists, overrides compiled in
	expectURLToContain(t, s.URL+"/a", "overridden-foo")

	// foo/b is compiled in, default/b is overridden, return compiled in
	expectURLToContain(t, s.URL+"/b", "foo")

	// only exists as compiled in default/c so use that
	expectURLToContain(t, s.URL+"/c", "default")

	// only exists as overridden default/d so use that
	expectURLToContain(t, s.URL+"/d", "overridden-default")
}

func expectURLToContain(t *testing.T, url, exp string) {
	res, err := http.Get(url)
	if err != nil {
		t.Error(err)
		return
	}

	if res.StatusCode != 200 {
		t.Errorf("Got %s instead of 200 OK", res.Status)
		return
	}

	data, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Error(err)
		return
	}

	if string(data) != exp {
		t.Errorf("Got %q instead of %q on %q", data, exp, url)
		return
	}
}

func TestDirNames(t *testing.T) {
	names := dirNames("testdata")
	expected := []string{"default", "foo", "testfolder"}
	if diff, equal := messagediff.PrettyDiff(expected, names); !equal {
		t.Errorf("Unexpected dirNames return: %#v\n%s", names, diff)
	}
}

type httpTestCase struct {
	URL     string        // URL to check
	Code    int           // Expected result code
	Type    string        // Expected content type
	Prefix  string        // Expected result prefix
	Timeout time.Duration // Defaults to a second
}

func TestAPIServiceRequests(t *testing.T) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cases := []httpTestCase{
		// /rest/db
		{
			URL:    "/rest/db/completion?device=" + protocol.LocalDeviceID.String() + "&folder=default",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:  "/rest/db/file?folder=default&file=something",
			Code: 404,
		},
		{
			URL:    "/rest/db/ignores?folder=default",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/db/need?folder=default",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/db/status?folder=default",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/db/browse?folder=default",
			Code:   200,
			Type:   "application/json",
			Prefix: "null",
		},

		// /rest/stats
		{
			URL:    "/rest/stats/device",
			Code:   200,
			Type:   "application/json",
			Prefix: "null",
		},
		{
			URL:    "/rest/stats/folder",
			Code:   200,
			Type:   "application/json",
			Prefix: "null",
		},

		// /rest/svc
		{
			URL:    "/rest/svc/deviceid?id=" + protocol.LocalDeviceID.String(),
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/svc/lang",
			Code:   200,
			Type:   "application/json",
			Prefix: "[",
		},
		{
			URL:     "/rest/svc/report",
			Code:    200,
			Type:    "application/json",
			Prefix:  "{",
			Timeout: 5 * time.Second,
		},

		// /rest/system
		{
			URL:    "/rest/system/browse?current=~",
			Code:   200,
			Type:   "application/json",
			Prefix: "[",
		},
		{
			URL:    "/rest/system/config",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/config/insync",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/connections",
			Code:   200,
			Type:   "application/json",
			Prefix: "null",
		},
		{
			URL:    "/rest/system/discovery",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/error?since=0",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/ping",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/status",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/version",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/debug",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/log?since=0",
			Code:   200,
			Type:   "application/json",
			Prefix: "{",
		},
		{
			URL:    "/rest/system/log.txt?since=0",
			Code:   200,
			Type:   "text/plain",
			Prefix: "",
		},
	}

	for _, tc := range cases {
		t.Log("Testing", tc.URL, "...")
		testHTTPRequest(t, baseURL, tc, testAPIKey)
	}
}

// testHTTPRequest tries the given test case, comparing the result code,
// content type, and result prefix.
func testHTTPRequest(t *testing.T, baseURL string, tc httpTestCase, apikey string) {
	timeout := time.Second
	if tc.Timeout > 0 {
		timeout = tc.Timeout
	}
	cli := &http.Client{
		Timeout: timeout,
	}

	req, err := http.NewRequest("GET", baseURL+tc.URL, nil)
	if err != nil {
		t.Errorf("Unexpected error requesting %s: %v", tc.URL, err)
		return
	}
	req.Header.Set("X-API-Key", apikey)

	resp, err := cli.Do(req)
	if err != nil {
		t.Errorf("Unexpected error requesting %s: %v", tc.URL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != tc.Code {
		t.Errorf("Get on %s should have returned status code %d, not %s", tc.URL, tc.Code, resp.Status)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, tc.Type) {
		t.Errorf("The content type on %s should be %q, not %q", tc.URL, tc.Type, ct)
		return
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Unexpected error reading %s: %v", tc.URL, err)
		return
	}

	if !bytes.HasPrefix(data, []byte(tc.Prefix)) {
		t.Errorf("Returned data from %s does not have prefix %q: %s", tc.URL, tc.Prefix, data)
		return
	}
}

func TestHTTPLogin(t *testing.T) {
	cfg := new(mockedConfig)
	cfg.gui.User = "üser"
	cfg.gui.Password = "$2a$10$IdIZTxTg/dCNuNEGlmLynOjqg4B1FvDKuIV5e0BB3pnWVHNb8.GSq" // bcrypt of "räksmörgås" in UTF-8
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify rejection when not using authorization

	req, _ := http.NewRequest("GET", baseURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected non-401 return code %d for unauthed request", resp.StatusCode)
	}

	// Verify that incorrect password is rejected

	req.SetBasicAuth("üser", "rksmrgs")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected non-401 return code %d for incorrect password", resp.StatusCode)
	}

	// Verify that incorrect username is rejected

	req.SetBasicAuth("user", "räksmörgås") // string literals in Go source code are in UTF-8
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Unexpected non-401 return code %d for incorrect username", resp.StatusCode)
	}

	// Verify that UTF-8 auth works

	req.SetBasicAuth("üser", "räksmörgås") // string literals in Go source code are in UTF-8
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected non-200 return code %d for authed request (UTF-8)", resp.StatusCode)
	}

	// Verify that ISO-8859-1 auth

	req.SetBasicAuth("\xfcser", "r\xe4ksm\xf6rg\xe5s") // escaped ISO-8859-1
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected non-200 return code %d for authed request (ISO-8859-1)", resp.StatusCode)
	}
}

func startHTTP(cfg *mockedConfig) (string, error) {
	model := new(mockedModel)
	httpsCertFile := "../../test/h1/https-cert.pem"
	httpsKeyFile := "../../test/h1/https-key.pem"
	assetDir := "../../gui"
	eventSub := new(mockedEventSub)
	diskEventSub := new(mockedEventSub)
	discoverer := new(mockedCachingMux)
	connections := new(mockedConnections)
	errorLog := new(mockedLoggerRecorder)
	systemLog := new(mockedLoggerRecorder)
	cpu := new(mockedCPUService)
	addrChan := make(chan string)

	// Instantiate the API service
	svc := newAPIService(protocol.LocalDeviceID, cfg, httpsCertFile, httpsKeyFile, assetDir, model,
		eventSub, diskEventSub, discoverer, connections, errorLog, systemLog, cpu)
	svc.started = addrChan

	// Actually start the API service
	supervisor := suture.NewSimple("API test")
	supervisor.Add(svc)
	supervisor.ServeBackground()

	// Make sure the API service is listening, and get the URL to use.
	addr := <-addrChan
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("Weird address from API service: %v", err)
	}

	host, _, _ := net.SplitHostPort(cfg.gui.RawAddress)
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	baseURL := fmt.Sprintf("http://%s", net.JoinHostPort(host, strconv.Itoa(tcpAddr.Port)))

	return baseURL, nil
}

func TestCSRFRequired(t *testing.T) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal("Unexpected error from getting base URL:", err)
	}

	cli := &http.Client{
		Timeout: time.Second,
	}

	// Getting the base URL (i.e. "/") should succeed.

	resp, err := cli.Get(baseURL)
	if err != nil {
		t.Fatal("Unexpected error from getting base URL:", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("Getting base URL should succeed, not", resp.Status)
	}

	// Find the returned CSRF token for future use

	var csrfTokenName, csrfTokenValue string
	for _, cookie := range resp.Cookies() {
		if strings.HasPrefix(cookie.Name, "CSRF-Token") {
			csrfTokenName = cookie.Name
			csrfTokenValue = cookie.Value
			break
		}
	}

	// Calling on /rest without a token should fail

	resp, err = cli.Get(baseURL + "/rest/system/config")
	if err != nil {
		t.Fatal("Unexpected error from getting /rest/system/config:", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatal("Getting /rest/system/config without CSRF token should fail, not", resp.Status)
	}

	// Calling on /rest with a token should succeed

	req, _ := http.NewRequest("GET", baseURL+"/rest/system/config", nil)
	req.Header.Set("X-"+csrfTokenName, csrfTokenValue)
	resp, err = cli.Do(req)
	if err != nil {
		t.Fatal("Unexpected error from getting /rest/system/config:", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("Getting /rest/system/config with CSRF token should succeed, not", resp.Status)
	}

	// Calling on /rest with the API key should succeed

	req, _ = http.NewRequest("GET", baseURL+"/rest/system/config", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	resp, err = cli.Do(req)
	if err != nil {
		t.Fatal("Unexpected error from getting /rest/system/config:", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("Getting /rest/system/config with API key should succeed, not", resp.Status)
	}
}

func TestRandomString(t *testing.T) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cli := &http.Client{
		Timeout: time.Second,
	}

	// The default should be to return a 32 character random string

	for _, url := range []string{"/rest/svc/random/string", "/rest/svc/random/string?length=-1", "/rest/svc/random/string?length=yo"} {
		req, _ := http.NewRequest("GET", baseURL+url, nil)
		req.Header.Set("X-API-Key", testAPIKey)
		resp, err := cli.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		var res map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			t.Fatal(err)
		}
		if len(res["random"]) != 32 {
			t.Errorf("Expected 32 random characters, got %q of length %d", res["random"], len(res["random"]))
		}
	}

	// We can ask for a different length if we like

	req, _ := http.NewRequest("GET", baseURL+"/rest/svc/random/string?length=27", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	var res map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if len(res["random"]) != 27 {
		t.Errorf("Expected 27 random characters, got %q of length %d", res["random"], len(res["random"]))
	}
}

func TestConfigPostOK(t *testing.T) {
	cfg := bytes.NewBuffer([]byte(`{
		"version": 15,
		"folders": [
			{"id": "foo"}
		]
	}`))

	resp, err := testConfigPost(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Error("Expected 200 OK, not", resp.Status)
	}
}

func TestConfigPostDupFolder(t *testing.T) {
	cfg := bytes.NewBuffer([]byte(`{
		"version": 15,
		"folders": [
			{"id": "foo"},
			{"id": "foo"}
		]
	}`))

	resp, err := testConfigPost(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Error("Expected 400 Bad Request, not", resp.Status)
	}
}

func testConfigPost(data io.Reader) (*http.Response, error) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		return nil, err
	}
	cli := &http.Client{
		Timeout: time.Second,
	}

	req, _ := http.NewRequest("POST", baseURL+"/rest/system/config", data)
	req.Header.Set("X-API-Key", testAPIKey)
	return cli.Do(req)
}

func TestHostCheck(t *testing.T) {
	// An API service bound to localhost should reject non-localhost host Headers

	cfg := new(mockedConfig)
	cfg.gui.RawAddress = "127.0.0.1:0"
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A normal HTTP get to the localhost-bound service should succeed

	resp, err := http.Get(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Regular HTTP get: expected 200 OK, not", resp.Status)
	}

	// A request with a suspicious Host header should fail

	req, _ := http.NewRequest("GET", baseURL, nil)
	req.Host = "example.com"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Error("Suspicious Host header: expected 403 Forbidden, not", resp.Status)
	}

	// A request with an explicit "localhost:8384" Host header should pass

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "localhost:8384"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Explicit localhost:8384: expected 200 OK, not", resp.Status)
	}

	// A request with an explicit "localhost" Host header (no port) should pass

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "localhost"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Explicit localhost: expected 200 OK, not", resp.Status)
	}

	// A server with InsecureSkipHostCheck set behaves differently

	cfg = new(mockedConfig)
	cfg.gui.RawAddress = "127.0.0.1:0"
	cfg.gui.InsecureSkipHostCheck = true
	baseURL, err = startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A request with a suspicious Host header should be allowed

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "example.com"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Incorrect host header, check disabled: expected 200 OK, not", resp.Status)
	}

	// A server bound to a wildcard address also doesn't do the check

	cfg = new(mockedConfig)
	cfg.gui.RawAddress = "0.0.0.0:0"
	cfg.gui.InsecureSkipHostCheck = true
	baseURL, err = startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A request with a suspicious Host header should be allowed

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "example.com"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Incorrect host header, wildcard bound: expected 200 OK, not", resp.Status)
	}

	// This should all work over IPv6 as well

	cfg = new(mockedConfig)
	cfg.gui.RawAddress = "[::1]:0"
	baseURL, err = startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// A normal HTTP get to the localhost-bound service should succeed

	resp, err = http.Get(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Regular HTTP get (IPv6): expected 200 OK, not", resp.Status)
	}

	// A request with a suspicious Host header should fail

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "example.com"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Error("Suspicious Host header (IPv6): expected 403 Forbidden, not", resp.Status)
	}

	// A request with an explicit "localhost:8384" Host header should pass

	req, _ = http.NewRequest("GET", baseURL, nil)
	req.Host = "localhost:8384"
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Error("Explicit localhost:8384 (IPv6): expected 200 OK, not", resp.Status)
	}
}

func TestAddressIsLocalhost(t *testing.T) {
	testcases := []struct {
		address string
		result  bool
	}{
		// These are all valid localhost addresses
		{"localhost", true},
		{"LOCALHOST", true},
		{"::1", true},
		{"127.0.0.1", true},
		{"localhost:8080", true},
		{"LOCALHOST:8000", true},
		{"[::1]:8080", true},
		{"127.0.0.1:8080", true},

		// These are all non-localhost addresses
		{"example.com", false},
		{"example.com:8080", false},
		{"192.0.2.10", false},
		{"192.0.2.10:8080", false},
		{"0.0.0.0", false},
		{"0.0.0.0:8080", false},
		{"::", false},
		{"[::]:8080", false},
		{":8080", false},
	}

	for _, tc := range testcases {
		result := addressIsLocalhost(tc.address)
		if result != tc.result {
			t.Errorf("addressIsLocalhost(%q)=%v, expected %v", tc.address, result, tc.result)
		}
	}
}

func TestAccessControlAllowOriginHeader(t *testing.T) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cli := &http.Client{
		Timeout: time.Second,
	}

	req, _ := http.NewRequest("GET", baseURL+"/rest/system/status", nil)
	req.Header.Set("X-API-Key", testAPIKey)
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("GET on /rest/system/status should succeed, not", resp.Status)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("GET on /rest/system/status should return a 'Access-Control-Allow-Origin: *' header")
	}
}

func TestOptionsRequest(t *testing.T) {
	const testAPIKey = "foobarbaz"
	cfg := new(mockedConfig)
	cfg.gui.APIKey = testAPIKey
	baseURL, err := startHTTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cli := &http.Client{
		Timeout: time.Second,
	}

	req, _ := http.NewRequest("OPTIONS", baseURL+"/rest/system/status", nil)
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatal("OPTIONS on /rest/system/status should succeed, not", resp.Status)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("OPTIONS on /rest/system/status should return a 'Access-Control-Allow-Origin: *' header")
	}
	if resp.Header.Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Fatal("OPTIONS on /rest/system/status should return a 'Access-Control-Allow-Methods: GET, POST' header")
	}
	if resp.Header.Get("Access-Control-Allow-Headers") != "Content-Type, X-API-Key" {
		t.Fatal("OPTIONS on /rest/system/status should return a 'Access-Control-Allow-Headers: Content-Type, X-API-KEY' header")
	}
}

func TestEventMasks(t *testing.T) {
	cfg := new(mockedConfig)
	defSub := new(mockedEventSub)
	diskSub := new(mockedEventSub)
	svc := newAPIService(protocol.LocalDeviceID, cfg, "", "", "", nil, defSub, diskSub, nil, nil, nil, nil, nil)

	if mask := svc.getEventMask(""); mask != defaultEventMask {
		t.Errorf("incorrect default mask %x != %x", int64(mask), int64(defaultEventMask))
	}

	expected := events.FolderSummary | events.LocalChangeDetected
	if mask := svc.getEventMask("FolderSummary,LocalChangeDetected"); mask != expected {
		t.Errorf("incorrect parsed mask %x != %x", int64(mask), int64(expected))
	}

	expected = 0
	if mask := svc.getEventMask("WeirdEvent,something else that doesn't exist"); mask != expected {
		t.Errorf("incorrect parsed mask %x != %x", int64(mask), int64(expected))
	}

	if res := svc.getEventSub(defaultEventMask); res != defSub {
		t.Errorf("should have returned the given default event sub")
	}
	if res := svc.getEventSub(diskEventMask); res != diskSub {
		t.Errorf("should have returned the given disk event sub")
	}
	if res := svc.getEventSub(events.LocalIndexUpdated); res == nil || res == defSub || res == diskSub {
		t.Errorf("should have returned a valid, non-default event sub")
	}
}

func TestBrowse(t *testing.T) {
	pathSep := string(os.PathSeparator)

	tmpDir, err := ioutil.TempDir("", "syncthing")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.Mkdir(filepath.Join(tmpDir, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(tmpDir, "file"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// We expect completion to return the full path to the completed
	// directory, with an ending slash.
	dirPath := filepath.Join(tmpDir, "dir") + pathSep

	cases := []struct {
		current string
		returns []string
	}{
		// The direcotory without slash is completed to one with slash.
		{tmpDir, []string{tmpDir + pathSep}},
		// With slash it's completed to its contents.
		// Dirs are given pathSeps.
		// Files are not returned.
		{tmpDir + pathSep, []string{dirPath}},
		// Globbing is automatic based on prefix.
		{tmpDir + pathSep + "d", []string{dirPath}},
		{tmpDir + pathSep + "di", []string{dirPath}},
		{tmpDir + pathSep + "dir", []string{dirPath}},
		{tmpDir + pathSep + "f", nil},
		{tmpDir + pathSep + "q", nil},
	}

	for _, tc := range cases {
		ret := browseFiles(tc.current, fs.FilesystemTypeBasic)
		if !equalStrings(ret, tc.returns) {
			t.Errorf("browseFiles(%q) => %q, expected %q", tc.current, ret, tc.returns)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
