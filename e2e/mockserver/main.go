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

	"github.com/gorilla/mux"
)

type user struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type post struct {
	ID      string   `json:"id"`
	UserID  string   `json:"user_id"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
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
	log.Printf("mockserver listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
}

func (s *serverState) createUser(w http.ResponseWriter, req *http.Request) {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id := strconv.Itoa(s.nextUserID)
	s.nextUserID++
	user := user{ID: id, Email: payload.Email, Password: payload.Password}
	s.users[id] = user
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         id,
		"email":      user.Email,
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
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, usr := range s.users {
		if usr.Email == payload.Email && usr.Password == payload.Password {
			token := fmt.Sprintf("token-%s", usr.ID)
			s.tokens[token] = usr.ID
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
	created := post{ID: id, UserID: userID, Title: payload.Title, Content: payload.Content, Tags: payload.Tags}
	s.posts[id] = created
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": created.ID, "title": created.Title, "content": created.Content, "tags": created.Tags})
}

func (s *serverState) getPost(w http.ResponseWriter, req *http.Request) {
	if _, ok := s.authenticate(req); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "invalid token"})
		return
	}
	id := mux.Vars(req)["id"]
	s.mu.Lock()
	defer s.mu.Unlock()
	post, exists := s.posts[id]
	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": post.ID, "title": post.Title, "content": post.Content, "tags": post.Tags})
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

	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "invalid json"})
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id := strconv.Itoa(s.nextUserID)
	s.nextUserID++
	user := user{ID: id, Email: payload.Email, Password: payload.Password}
	s.users[id] = user
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         id,
		"email":      payload.Email,
		"created_at": "2026-01-01T00:00:00Z",
	})
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
