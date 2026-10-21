package main

import "net/http"

// diagnosticsQueryToken is sent in the query string of the page's fetch.
// The browser e2e asserts it never reaches network.jsonl: Tales strips query
// strings from every URL it records (issue #83).
const diagnosticsQueryToken = "s3cr3t-query-token"

// diagnosticsPage backs the browser failure-diagnostics e2e (issue #83). On
// load it logs to the console, throws an uncaught error and fetches
// /web/diagnostics/ping with a token in the query string, then shows the
// answer. A failed step on it must leave console.jsonl, network.jsonl and
// state.json behind with each of those facts in them.
const diagnosticsPage = `<!doctype html>
<html>
  <head>
    <title>Diagnostics</title>
  </head>
  <body>
    <h1 data-testid="diagnostics.title">Diagnostics</h1>
    <input data-testid="diagnostics.query" class="search wide" name="q" type="search" />
    <p data-testid="diagnostics.pong" hidden></p>
    <script>
      console.log("diagnostics page ready", { step: 1, name: "ping" });
      console.warn("diagnostics warning");

      fetch("/web/diagnostics/ping?token=` + diagnosticsQueryToken + `")
        .then(function (response) { return response.json(); })
        .then(function (body) {
          var pong = document.querySelector("[data-testid='diagnostics.pong']");
          pong.textContent = body.pong;
          pong.hidden = false;
          console.info("diagnostics pong received");
        });

      setTimeout(function explode() {
        throw new Error("diagnostics boom");
      }, 0);
    </script>
  </body>
</html>
`

func (s *serverState) webDiagnostics(w http.ResponseWriter, _ *http.Request) {
	writeHTML(w, diagnosticsPage)
}

func (s *serverState) webDiagnosticsPing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"pong":"pong"}`))
}
