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

// uploadPage backs the browser `upload_file` action e2e. The file input is
// hidden behind a styled label — the shape real apps ship — so the scenario
// also pins that upload_file does not require the input to be visible. The
// change handler reads the picked files client-side and hashes them with
// SubtleCrypto, mirroring the pre-upload digest flow the action exists to
// make testable.
const uploadPage = `<!doctype html>
<html>
  <head>
    <title>Upload</title>
  </head>
  <body>
    <h1 data-testid="upload.title">Upload</h1>
    <label data-testid="upload.open" for="file">Choose a file</label>
    <input data-testid="upload.input" id="file" name="document" type="file" multiple style="display: none" />
    <p data-testid="upload.count"></p>
    <p data-testid="upload.names"></p>
    <p data-testid="upload.size"></p>
    <p data-testid="upload.sha256"></p>
    <p data-testid="upload.ready" style="display: none">ready</p>
    <script>
      document.getElementById("file").addEventListener("change", async function (event) {
        var files = Array.prototype.slice.call(event.target.files);

        document.querySelector("[data-testid='upload.count']").textContent = String(files.length);
        document.querySelector("[data-testid='upload.names']").textContent =
          files.map(function (f) { return f.name; }).join(",");
        document.querySelector("[data-testid='upload.size']").textContent =
          String(files.reduce(function (total, f) { return total + f.size; }, 0));

        var buffer = await files[0].arrayBuffer();
        var digest = await crypto.subtle.digest("SHA-256", buffer);
        document.querySelector("[data-testid='upload.sha256']").textContent =
          Array.prototype.map
            .call(new Uint8Array(digest), function (b) { return b.toString(16).padStart(2, "0"); })
            .join("");

        document.querySelector("[data-testid='upload.ready']").style.display = "block";
      });
    </script>
  </body>
</html>
`

func (s *serverState) webUpload(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(uploadPage))
}

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
