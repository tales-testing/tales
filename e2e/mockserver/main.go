package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// webhookSignedSecret is the HMAC key shared with the matching .tales scenarios
// via config.webhook_secret. Hardcoded in tests; never log it.
const webhookSignedSecret = "test-secret"

type user struct {
	id     string
	email  string
	secret string
}

type post struct {
	id      string
	userID  string
	title   string
	content string
	tags    []string
}

type serverState struct {
	mu                sync.Mutex
	nextUserID        int
	nextPostID        int
	users             map[string]user
	posts             map[string]post
	tokens            map[string]string
	verificationCodes map[string]string
	mailPolls         map[string]int
	markers           map[string]map[string]interface{}
}

func newState() *serverState {
	return &serverState{
		nextUserID:        1,
		nextPostID:        1,
		users:             map[string]user{},
		posts:             map[string]post{},
		tokens:            map[string]string{},
		verificationCodes: map[string]string{},
		mailPolls:         map[string]int{},
		markers:           map[string]map[string]interface{}{},
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "1337"
	}

	state := newState()
	r := mux.NewRouter()

	r.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	}).Methods(http.MethodGet)

	r.HandleFunc("/users", state.createUser).Methods(http.MethodPost)
	r.HandleFunc("/users/{id}", state.deleteUser).Methods(http.MethodDelete)
	r.HandleFunc("/auth", state.auth).Methods(http.MethodPost)
	r.HandleFunc("/basic-auth", state.basicAuth).Methods(http.MethodGet)
	r.HandleFunc("/form-echo", state.formEcho).Methods(http.MethodPost)
	r.HandleFunc("/mail/messages", state.mailMessages).Methods(http.MethodGet)
	r.HandleFunc("/verify-email", state.verifyEmail).Methods(http.MethodPost)
	r.HandleFunc("/markers", state.createMarker).Methods(http.MethodPost)
	r.HandleFunc("/markers/{id}", state.getMarker).Methods(http.MethodGet)
	r.HandleFunc("/webhook/signed", state.signedWebhook).Methods(http.MethodPost)
	r.HandleFunc("/blog/posts", state.createPost).Methods(http.MethodPost)
	r.HandleFunc("/blog/posts/{id}", state.getPost).Methods(http.MethodGet)
	r.HandleFunc("/blog/posts/{id}", state.deletePost).Methods(http.MethodDelete)
	r.HandleFunc("/users.v1.UserService/CreateUser", state.connectCreateUser).Methods(http.MethodPost)
	r.HandleFunc("/users.v1.UserService/GetUser", state.connectGetUser).Methods(http.MethodPost)

	addr := ":" + port
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Print("mockserver listening")

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func (s *serverState) createUser(w http.ResponseWriter, req *http.Request) {
	payload, err := decodeCredentialPayload(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := strconv.Itoa(s.nextUserID)
	s.nextUserID++
	usr := user{id: id, email: payload.email, secret: payload.secret}
	s.users[id] = usr
	s.verificationCodes[usr.email] = verificationCode(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         id,
		"email":      usr.email,
		"created_at": "2026-01-01T00:00:00Z",
	})
}

func (s *serverState) deleteUser(w http.ResponseWriter, req *http.Request) {
	if _, ok := s.authenticate(req); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid token"})

		return
	}

	id := mux.Vars(req)["id"]

	s.mu.Lock()
	defer s.mu.Unlock()

	usr, exists := s.users[id]
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})

		return
	}

	delete(s.users, id)
	delete(s.verificationCodes, usr.email)
	delete(s.mailPolls, usr.email)
	writeJSON(w, http.StatusNoContent, map[string]interface{}{"deleted": true})
}

func (s *serverState) auth(w http.ResponseWriter, req *http.Request) {
	payload, err := decodeCredentialPayload(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, usr := range s.users {
		if usr.email == payload.email && usr.secret == payload.secret {
			token := fmt.Sprintf("token-%s", usr.id)
			s.tokens[token] = usr.id
			writeJSON(w, http.StatusOK, map[string]interface{}{"access_token": token})

			return
		}
	}

	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid credentials"})
}

func (s *serverState) basicAuth(w http.ResponseWriter, req *http.Request) {
	username, password, ok := req.BasicAuth()
	if ok && username == "admin" && password == "secret" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"authenticated": true})

		return
	}

	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "unauthorized"})
}

func (s *serverState) formEcho(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})

		return
	}

	form := map[string]interface{}{}

	for key, values := range req.PostForm {
		if len(values) > 0 {
			form[key] = values[0]
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"form": form})
}

func (s *serverState) mailMessages(w http.ResponseWriter, req *http.Request) {
	email := req.URL.Query().Get("to")
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "missing to"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.mailPolls[email]++

	code, ok := s.verificationCodes[email]
	if !ok || s.mailPolls[email] < 2 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"messages": []interface{}{}})

		return
	}

	w.Header().Set("X-Test-Verification-Code", code)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"to":      email,
				"subject": "Verify your account",
				"body":    fmt.Sprintf("Your verification code is %s", code),
			},
		},
	})
}

func (s *serverState) verifyEmail(w http.ResponseWriter, req *http.Request) {
	payload := map[string]string{}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	email := payload["email"]
	code := payload["code"]

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.verificationCodes[email] != code {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid verification code"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"verified": true})
}

func (s *serverState) createPost(w http.ResponseWriter, req *http.Request) {
	userID, ok := s.authenticate(req)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid token"})

		return
	}

	var payload struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := strconv.Itoa(s.nextPostID)
	s.nextPostID++
	created := post{id: id, userID: userID, title: payload.Title, content: payload.Content, tags: payload.Tags}
	s.posts[id] = created
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": created.id, "title": created.title, "content": created.content, "tags": created.tags})
}

func (s *serverState) getPost(w http.ResponseWriter, req *http.Request) {
	if _, ok := s.authenticate(req); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid token"})

		return
	}

	id := mux.Vars(req)["id"]

	s.mu.Lock()
	defer s.mu.Unlock()

	pst, exists := s.posts[id]
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": pst.id, "title": pst.title, "content": pst.content, "tags": pst.tags})
}

func (s *serverState) deletePost(w http.ResponseWriter, req *http.Request) {
	if _, ok := s.authenticate(req); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid token"})

		return
	}

	id := mux.Vars(req)["id"]

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.posts[id]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})

		return
	}

	delete(s.posts, id)
	writeJSON(w, http.StatusNoContent, map[string]interface{}{"deleted": true})
}

// createMarker stores a JSON body under a client-supplied id. It is used by
// the file-order E2E scenario: one step writes a marker at a known id and a
// later step reads it back, without referencing the writer step's result, so
// the read can only succeed if steps run in .tales file order.
func (s *serverState) createMarker(w http.ResponseWriter, req *http.Request) {
	body := map[string]interface{}{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	id, ok := body["id"].(string)
	if !ok || id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "id is required"})

		return
	}

	s.mu.Lock()
	s.markers[id] = body
	s.mu.Unlock()

	writeJSON(w, http.StatusCreated, body)
}

// signedWebhook validates an HMAC-SHA256 signed POST against the shared
// secret. It expects an X-Anchorify-Signature header formatted as
// "t=<unix>,v1=<hex>" and computes its own digest over "<t>.<raw_body>" to
// compare in constant time. Timestamps outside a ±5 minute window are
// rejected as expired. Error responses never echo back the expected
// signature so the secret is never reflected in test output.
func (s *serverState) signedWebhook(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "cannot read body"})

		return
	}

	header := req.Header.Get("X-Anchorify-Signature")
	if header == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "missing signature"})

		return
	}

	tsStr, sigStr, ok := parseSignatureHeader(header)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid signature header"})

		return
	}

	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid timestamp"})

		return
	}

	skew := time.Now().Unix() - ts
	if skew < 0 {
		skew = -skew
	}

	if skew > 300 {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "expired"})

		return
	}

	mac := hmac.New(sha256.New, []byte(webhookSignedSecret))
	mac.Write([]byte(tsStr + "." + string(body)))
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(sigStr)) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid signature"})

		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// parseSignatureHeader splits an "t=<unix>,v1=<hex>" header. It tolerates
// the parts arriving in either order and ignores unknown keys.
func parseSignatureHeader(value string) (string, string, bool) {
	var (
		ts  string
		sig string
	)

	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)

		k, v, found := strings.Cut(part, "=")
		if !found {
			continue
		}

		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}

	if ts == "" || sig == "" {
		return "", "", false
	}

	return ts, sig, true
}

// getMarker returns a marker previously stored by createMarker.
func (s *serverState) getMarker(w http.ResponseWriter, req *http.Request) {
	id := mux.Vars(req)["id"]

	s.mu.Lock()
	marker, exists := s.markers[id]
	s.mu.Unlock()

	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})

		return
	}

	writeJSON(w, http.StatusOK, marker)
}

// connectGetUser simulates a ConnectRPC / protobuf JSON response where
// fields holding default values may be omitted from the JSON payload.
// The `mode` field in the request body controls which shape is returned:
//   - "minimal": id only (mimicking omitted default-valued fields).
//   - "full":    id plus default-valued role ("ROLE_UNSPECIFIED"),
//     permissions ([]), display_name (""), and a metadata object with a
//     populated value — used to exercise optional(any()) on a present key.
func (s *serverState) connectGetUser(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Connect-Protocol-Version") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "missing Connect-Protocol-Version"})

		return
	}

	payload := map[string]string{}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	id := payload["id"]
	if id == "" {
		id = "user_123"
	}

	switch payload["mode"] {
	case "full":
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":           id,
			"role":         "ROLE_UNSPECIFIED",
			"permissions":  []interface{}{},
			"display_name": "",
			"metadata":     map[string]interface{}{"source": "mock"},
		})

		return
	default:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id": id,
		})

		return
	}
}

func (s *serverState) connectCreateUser(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("Connect-Protocol-Version") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "missing Connect-Protocol-Version"})

		return
	}

	payload, err := decodeCredentialPayload(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := strconv.Itoa(s.nextUserID)
	s.nextUserID++
	usr := user{id: id, email: payload.email, secret: payload.secret}
	s.users[id] = usr
	s.verificationCodes[usr.email] = verificationCode(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         id,
		"email":      payload.email,
		"created_at": "2026-01-01T00:00:00Z",
	})
}

type credentialPayload struct {
	email  string
	secret string
}

func decodeCredentialPayload(req *http.Request) (*credentialPayload, error) {
	data := map[string]interface{}{}
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode credential payload: %w", err)
	}

	email, err := requiredStringField(data, "email")
	if err != nil {
		return nil, err
	}

	secret, err := requiredStringField(data, "password")
	if err != nil {
		return nil, err
	}

	return &credentialPayload{email: email, secret: secret}, nil
}

func requiredStringField(data map[string]interface{}, key string) (string, error) {
	value, ok := data[key]
	if !ok || value == nil {
		return "", fmt.Errorf("credential payload field %q is required", key)
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("credential payload field %q must be a string", key)
	}

	return text, nil
}

func (s *serverState) authenticate(req *http.Request) (string, bool) {
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return "", false
	}

	token := strings.TrimPrefix(auth, "Bearer ")

	s.mu.Lock()
	defer s.mu.Unlock()

	userID, ok := s.tokens[token]

	return userID, ok
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func verificationCode(id string) string {
	value, err := strconv.Atoi(id)
	if err != nil {
		return "A00000"
	}

	return fmt.Sprintf("A%05d", value)
}
