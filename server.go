package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const basicAuthUsername = "vicuna"

type managerAPI interface {
	Ports() ([]portInfo, error)
	Connect(serialConfig) error
	Disconnect()
	Status() serialStatus
	Signals() modemSignals
	Write([]byte) error
	SetSignals(*bool, *bool) error
	Break(time.Duration) error
	ResetBuffers() error
}

type apiServer struct {
	manager  managerAPI
	hub      *hub
	static   http.FileSystem
	hardware *hardwareRegistry
	config   deploymentConfig
}

func newAPIServer(manager managerAPI, events *hub, static http.FileSystem, config deploymentConfig) *apiServer {
	return &apiServer{manager: manager, hub: events, static: static, hardware: newHardwareRegistry(manager), config: config}
}

func (s *apiServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ports", s.handlePorts)
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/signals", s.handleSignals)
	mux.HandleFunc("GET /api/hardware", s.handleHardware)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("POST /api/connect", s.handleConnect)
	mux.HandleFunc("POST /api/disconnect", s.handleDisconnect)
	mux.HandleFunc("POST /api/send", s.handleSend)
	mux.HandleFunc("POST /api/control", s.handleControl)
	mux.HandleFunc("POST /api/hardware/control", s.handleHardwareControl)
	mux.Handle("/", http.FileServer(s.static))
	handler := sameOrigin(mux)
	if s.config.Password != "" {
		handler = passwordProtection(s.config.Password, handler)
	}
	return securityHeaders(handler)
}

func (s *apiServer) handlePorts(w http.ResponseWriter, _ *http.Request) {
	ports, err := s.manager.Ports()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ports": ports})
}

func (s *apiServer) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.Status())
}

func (s *apiServer) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.config)
}

func (s *apiServer) handleSignals(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.manager.Signals())
}

func (s *apiServer) handleHardware(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"modules": s.hardware.Definitions()})
}

func (s *apiServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	var config serialConfig
	if err := readJSON(r, &config); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.manager.Connect(config); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.manager.Status())
}

func (s *apiServer) handleDisconnect(w http.ResponseWriter, _ *http.Request) {
	s.manager.Disconnect()
	writeJSON(w, http.StatusOK, s.manager.Status())
}

func (s *apiServer) handleSend(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
	}
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	data, err := decodePayload(request.Encoding, request.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("payload is empty"))
		return
	}
	if err := s.manager.Write(data); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"written": len(data)})
}

func (s *apiServer) handleControl(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action     string `json:"action"`
		DTR        *bool  `json:"dtr"`
		RTS        *bool  `json:"rts"`
		DurationMS int    `json:"durationMs"`
	}
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var err error
	switch request.Action {
	case "signals":
		err = s.manager.SetSignals(request.DTR, request.RTS)
	case "break":
		if request.DurationMS == 0 {
			request.DurationMS = 250
		}
		err = s.manager.Break(time.Duration(request.DurationMS) * time.Millisecond)
	case "reset-buffers":
		err = s.manager.ResetBuffers()
	default:
		err = errors.New("action must be signals, break, or reset-buffers")
	}
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *apiServer) handleHardwareControl(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Module  string `json:"module"`
		Control string `json:"control"`
		Value   bool   `json:"value"`
	}
	if err := readJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.hardware.Set(request.Module, request.Control, request.Value); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *apiServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	events, unsubscribe := s.hub.subscribe()
	defer unsubscribe()
	status, _ := json.Marshal(event{Type: "status", Time: time.Now(), Status: pointerTo(s.manager.Status())})
	fmt.Fprintf(w, "data: %s\n\n", status)
	signals, _ := json.Marshal(event{Type: "signals", Time: time.Now(), Signals: pointerTo(s.manager.Signals())})
	fmt.Fprintf(w, "data: %s\n\n", signals)
	flusher.Flush()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case value, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(value)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func decodePayload(encoding, value string) ([]byte, error) {
	switch strings.ToLower(encoding) {
	case "", "text":
		return []byte(value), nil
	case "base64":
		data, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 payload: %w", err)
		}
		return data, nil
	case "hex":
		cleaned := strings.NewReplacer("0x", "", "0X", "", ",", " ", ":", " ", "_", " ").Replace(value)
		cleaned = strings.Join(strings.Fields(cleaned), "")
		if len(cleaned)%2 != 0 {
			return nil, errors.New("hex payload must contain complete byte pairs")
		}
		data, err := hex.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("invalid hex payload: %w", err)
		}
		return data, nil
	default:
		return nil, errors.New("encoding must be text, hex, or base64")
	}
}

func readJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func pointerTo[T any](value T) *T { return &value }

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
					writeError(w, http.StatusForbidden, errors.New("cross-origin request rejected"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func passwordProtection(password string, next http.Handler) http.Handler {
	expectedUsername := sha256.Sum256([]byte(basicAuthUsername))
	expectedPassword := sha256.Sum256([]byte(password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, suppliedPassword, ok := r.BasicAuth()
		usernameHash := sha256.Sum256([]byte(username))
		passwordHash := sha256.Sum256([]byte(suppliedPassword))
		usernameMatches := subtle.ConstantTimeCompare(usernameHash[:], expectedUsername[:])
		passwordMatches := subtle.ConstantTimeCompare(passwordHash[:], expectedPassword[:])
		if !ok || usernameMatches&passwordMatches != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Vicuña", charset="UTF-8"`)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
