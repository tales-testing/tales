package main

import (
	"html"
	"net/http"
	"net/url"
)

const loginPage = `<!doctype html>
<html>
  <head>
    <title>Login</title>
    <meta name="csrf-token" content="csrf-demo-token">
  </head>
  <body>
    <h1 data-testid="login.title">Login</h1>
    <form id="login-form" action="/web/login" method="post">
      <input data-testid="login.email" name="email" />
      <input data-testid="login.password" name="password" type="password" />
      <button data-testid="login.submit" type="submit">Login</button>
    </form>
  </body>
</html>
`

const formPage = `<!doctype html>
<html>
  <head>
    <title>Form</title>
  </head>
  <body>
    <h1 data-testid="form.title">Form</h1>
    <form id="prefs-form">
      <select data-testid="form.country" name="country">
        <option value="US">United States</option>
        <option value="FR">France</option>
        <option value="JP">Japan</option>
      </select>
      <input data-testid="form.subscribe" type="checkbox" name="subscribe" />
      <input data-testid="form.tos" type="checkbox" name="tos" checked />
      <textarea data-testid="form.notes" name="notes"></textarea>
      <button data-testid="form.submit" type="submit">Save</button>
    </form>
  </body>
</html>
`

func (s *serverState) webLoginGet(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(loginPage))
}

func (s *serverState) webLoginPost(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}

	email := req.PostFormValue("email")
	password := req.PostFormValue("password")

	if email == "" || password == "" {
		http.Error(w, "credentials required", http.StatusBadRequest)

		return
	}

	location := "/web/dashboard?email=" + url.QueryEscape(email)
	http.Redirect(w, req, location, http.StatusFound)
}

func (s *serverState) webDashboard(w http.ResponseWriter, req *http.Request) {
	email := req.URL.Query().Get("email")
	if email == "" {
		email = "demo@example.com"
	}

	page := `<!doctype html>
<html>
  <head>
    <title>Dashboard</title>
    <meta name="csrf-token" content="csrf-demo-token">
  </head>
  <body>
    <h1 data-testid="dashboard.title">Dashboard</h1>
    <p data-testid="dashboard.email">` + html.EscapeString(email) + `</p>
    <a data-testid="dashboard.logout" href="/web/login">Logout</a>
  </body>
</html>
`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func (s *serverState) webForm(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(formPage))
}
