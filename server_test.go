package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

type fakeManager struct {
	status  serialStatus
	signals modemSignals
	writes  [][]byte
	dtr     *bool
	rts     *bool
}

func (f *fakeManager) Ports() ([]portInfo, error) {
	return []portInfo{{Name: "COM7", USB: true, VID: "1A86", PID: "55D3", Product: "USB Serial"}}, nil
}
func (f *fakeManager) Connect(config serialConfig) error {
	f.status = serialStatus{Connected: true, Config: &config}
	return nil
}
func (f *fakeManager) Disconnect()           { f.status = serialStatus{} }
func (f *fakeManager) Status() serialStatus  { return f.status }
func (f *fakeManager) Signals() modemSignals { return f.signals }
func (f *fakeManager) Write(value []byte) error {
	f.writes = append(f.writes, value)
	return nil
}
func (f *fakeManager) SetSignals(dtr, rts *bool) error {
	if dtr != nil {
		value := *dtr
		f.dtr = &value
	}
	if rts != nil {
		value := *rts
		f.rts = &value
	}
	return nil
}
func (f *fakeManager) Break(time.Duration) error { return nil }
func (f *fakeManager) ResetBuffers() error       { return nil }

func testHandler(t *testing.T, manager managerAPI) http.Handler {
	t.Helper()
	assets := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	return newAPIServer(manager, newHub(), http.FS(assets), defaultDeploymentConfig()).routes()
}

func TestDecodePayload(t *testing.T) {
	tests := []struct {
		name, encoding, input, want string
	}{
		{"text", "text", "hello\r\n", "hello\r\n"},
		{"spaced hex", "hex", "55 AA 00 ff", "\x55\xaa\x00\xff"},
		{"prefixed hex", "hex", "0x01, 0x02:03", "\x01\x02\x03"},
		{"base64", "base64", "AQID", "\x01\x02\x03"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodePayload(test.encoding, test.input)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestDecodePayloadRejectsOddHex(t *testing.T) {
	if _, err := decodePayload("hex", "ABC"); err == nil {
		t.Fatal("expected incomplete hex byte to be rejected")
	}
}

func TestSendEndpointPreservesBinaryBytes(t *testing.T) {
	manager := &fakeManager{status: serialStatus{Connected: true}}
	request := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader(`{"encoding":"hex","data":"00 FF 80"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://example.test")
	request.Host = "example.test"
	recorder := httptest.NewRecorder()
	testHandler(t, manager).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(manager.writes) != 1 || string(manager.writes[0]) != "\x00\xff\x80" {
		t.Fatalf("unexpected writes: %v", manager.writes)
	}
}

func TestCrossOriginMutationIsRejected(t *testing.T) {
	manager := &fakeManager{}
	request := httptest.NewRequest(http.MethodPost, "/api/disconnect", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://attacker.example")
	request.Host = "console.local:8080"
	recorder := httptest.NewRecorder()
	testHandler(t, manager).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", recorder.Code)
	}
}

func TestSecurityHeadersAndCachePolicy(t *testing.T) {
	handler := testHandler(t, &fakeManager{})

	apiRequest := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	apiRecorder := httptest.NewRecorder()
	handler.ServeHTTP(apiRecorder, apiRequest)
	if apiRecorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", apiRecorder.Code, apiRecorder.Body.String())
	}
	for name, expected := range map[string]string{
		"Cache-Control":                "no-store",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Permissions-Policy":           "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":              "no-referrer",
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
	} {
		if got := apiRecorder.Header().Get(name); got != expected {
			t.Errorf("%s = %q, want %q", name, got, expected)
		}
	}
	if policy := apiRecorder.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "frame-ancestors 'none'") || !strings.Contains(policy, "object-src 'none'") {
		t.Errorf("content security policy is incomplete: %q", policy)
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pageRecorder, pageRequest)
	if got := pageRecorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("static Cache-Control = %q, want no-cache", got)
	}
}

func TestPortsEndpoint(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/ports", nil)
	recorder := httptest.NewRecorder()
	testHandler(t, &fakeManager{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	var response struct {
		Ports []portInfo `json:"ports"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Ports) != 1 || response.Ports[0].Name != "COM7" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestConfigEndpoint(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	config := defaultDeploymentConfig()
	config.Configured = true
	config.Mode = "embedded"
	config.Theme = "light"
	config.Hardware = "generic-rs232"
	config.Serial.Port = "COM9"
	handler := newAPIServer(&fakeManager{}, newHub(), http.FS(assets), config).routes()
	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response deploymentConfig
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Configured || response.Mode != "embedded" || response.Theme != "light" || response.Hardware != "generic-rs232" || response.Serial.Port != "COM9" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestPasswordProtection(t *testing.T) {
	assets := fstest.MapFS{"index.html": {Data: []byte("ok")}}
	config := defaultDeploymentConfig()
	config.Configured = true
	config.Password = "correct horse battery staple"
	handler := newAPIServer(&fakeManager{}, newHub(), http.FS(assets), config).routes()

	tests := []struct {
		name, username, password string
		want                     int
	}{
		{name: "missing credentials", want: http.StatusUnauthorized},
		{name: "wrong password", username: basicAuthUsername, password: "wrong", want: http.StatusUnauthorized},
		{name: "wrong username", username: "admin", password: config.Password, want: http.StatusUnauthorized},
		{name: "correct credentials", username: basicAuthUsername, password: config.Password, want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			if test.username != "" || test.password != "" {
				request.SetBasicAuth(test.username, test.password)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status %d, want %d: %s", recorder.Code, test.want, recorder.Body.String())
			}
			if test.want == http.StatusUnauthorized && recorder.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("authentication challenge is missing")
			}
			if strings.Contains(recorder.Body.String(), config.Password) {
				t.Fatal("password leaked in response")
			}
		})
	}
}

func TestHardwareEndpointListsRegisteredModules(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/hardware", nil)
	recorder := httptest.NewRecorder()
	testHandler(t, &fakeManager{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Modules []hardwareDefinition `json:"modules"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Modules) != 2 || response.Modules[0].ID != "generic-rs232" || response.Modules[1].ID != "pt1" {
		t.Fatalf("unexpected modules: %+v", response.Modules)
	}
}
