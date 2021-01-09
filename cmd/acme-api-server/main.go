package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/gorilla/mux"
)

type User struct {
	ID          string `json:"id,omitempty"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	AccessToken string `json:"access_token,omitempty"`
}

type Post struct {
	ID      string   `json:"id,omitempty"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

func main() {
	users := map[string]*User{}
	authenticated := map[string]*User{}
	posts := map[string]*Post{}

	r := mux.NewRouter()

	r.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		b, _ := ioutil.ReadAll(r.Body)

		w.Write(b)
	}).Methods(http.MethodPost)

	r.HandleFunc("/register-bad", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		var buf bytes.Buffer
		r.Write(&buf)
		w.Write(buf.Bytes())
	}).Methods(http.MethodPost)

	r.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		user := &User{
			ID: uuid.New().String(),
		}
		if err := json.NewDecoder(r.Body).Decode(user); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		users[user.ID] = user

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(user)
	}).Methods(http.MethodPost)

	r.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		user := &User{}
		if err := json.NewDecoder(r.Body).Decode(user); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		resp := map[string]string{}

		for _, u := range users {
			if u.Email == user.Email && u.Password == user.Password {
				u.AccessToken = uuid.New().String()
				authenticated[u.AccessToken] = u

				resp["access_token"] = u.AccessToken

				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(resp)
				return
			}
		}

		w.WriteHeader(http.StatusUnauthorized)

	}).Methods(http.MethodPost)

	r.HandleFunc("/blog/posts", func(w http.ResponseWriter, r *http.Request) {
		_, err := getUserAuth(authenticated, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)

			return
		}

		post := &Post{
			ID: uuid.New().String(),
		}
		if err := json.NewDecoder(r.Body).Decode(post); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		posts[post.ID] = post

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(post)
	}).Methods(http.MethodPost)

	r.HandleFunc("/blog/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, err := getUserAuth(authenticated, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)

			return
		}

		params := mux.Vars(r)

		post, ok := posts[params["id"]]
		if !ok {
			http.Error(w, "post not found", http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(post)
	}).Methods(http.MethodGet)

	r.HandleFunc("/blog/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, err := getUserAuth(authenticated, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)

			return
		}

		params := mux.Vars(r)

		_, ok := posts[params["id"]]
		if !ok {
			http.Error(w, "post not found", http.StatusNotFound)

			return
		}

		delete(posts, params["id"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}).Methods(http.MethodDelete)

	r.HandleFunc("/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, err := getUserAuth(authenticated, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)

			return
		}

		params := mux.Vars(r)

		user, ok := users[params["id"]]
		if !ok {
			http.Error(w, "user not found", http.StatusNotFound)

			return
		}

		delete(users, params["id"])
		delete(authenticated, user.AccessToken)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"status": true})
	}).Methods(http.MethodDelete)

	http.Handle("/", r)

	log.Fatal(http.ListenAndServe(":1337", nil))
}

func getUserAuth(authenticated map[string]*User, r *http.Request) (*User, error) {
	value := r.Header.Get("Authorization")

	token := strings.Replace(value, "Bearer ", "", 2)

	if user, ok := authenticated[token]; ok {
		return user, nil
	}

	return nil, fmt.Errorf("user not found for token %s", token)
}
