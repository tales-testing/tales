package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

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
	mu         sync.Mutex
	nextUserID int
	nextPostID int
	users      map[string]user
	posts      map[string]post
	tokens     map[string]string
}

func newState() *serverState {
	return &serverState{
		nextUserID: 1,
		nextPostID: 1,
		users:      map[string]user{},
		posts:      map[string]post{},
		tokens:     map[string]string{},
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
	r.HandleFunc("/blog/posts", state.createPost).Methods(http.MethodPost)
	r.HandleFunc("/blog/posts/{id}", state.getPost).Methods(http.MethodGet)
	r.HandleFunc("/blog/posts/{id}", state.deletePost).Methods(http.MethodDelete)
	r.HandleFunc("/users.v1.UserService/CreateUser", state.connectCreateUser).Methods(http.MethodPost)

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

	if _, exists := s.users[id]; !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})

		return
	}

	delete(s.users, id)
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
	data := map[string]string{}
	if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode credential payload: %w", err)
	}

	return &credentialPayload{
		email:  data["email"],
		secret: data["password"],
	}, nil
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
