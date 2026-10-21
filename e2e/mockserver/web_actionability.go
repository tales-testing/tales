package main

import "net/http"

// The pages below back the browser actionability e2e (issue #82). Each one
// reproduces a way a pointer action used to be lost in silence: the click
// landed on something else, the action passed, and the scenario failed
// later on an unrelated wait.

// actionabilityVeilPage covers the whole page with a transparent veil for
// one second after load, the shape of a loading shield nobody sees on a
// screenshot. A click dispatched while it is there hits the veil.
const actionabilityVeilPage = `<!doctype html>
<html>
  <head>
    <title>Veil</title>
    <style>
      .veil { position: fixed; inset: 0; z-index: 10; background: transparent; }
    </style>
  </head>
  <body>
    <h1 data-testid="veil.title">Veil</h1>
    <button data-testid="veil.submit" type="button">Submit</button>
    <p data-testid="veil.done" hidden>submitted</p>
    <div class="veil" data-testid="veil.veil"></div>
    <script>
      document.querySelector("[data-testid='veil.submit']").addEventListener("click", function () {
        document.querySelector("[data-testid='veil.done']").hidden = false;
      });

      setTimeout(function () {
        document.querySelector(".veil").remove();
      }, 1000);
    </script>
  </body>
</html>
`

// actionabilitySlidingPage slides its submit button in from the left after
// a short delay. The button is laid out (so it counts as visible) while it
// is still off screen, and then moving: a click measured in that window
// lands where the button is not.
const actionabilitySlidingPage = `<!doctype html>
<html>
  <head>
    <title>Sliding</title>
    <style>
      body { overflow-x: hidden; }
      .slide { animation: slide-in 600ms ease-out 400ms both; }
      @keyframes slide-in {
        from { transform: translateX(-120vw); }
        to { transform: none; }
      }
    </style>
  </head>
  <body>
    <h1 data-testid="sliding.title">Sliding</h1>
    <button class="slide" data-testid="sliding.submit" type="button">Submit</button>
    <p data-testid="sliding.done" hidden>submitted</p>
    <script>
      document.querySelector("[data-testid='sliding.submit']").addEventListener("click", function () {
        document.querySelector("[data-testid='sliding.done']").hidden = false;
      });
    </script>
  </body>
</html>
`

// actionabilityStickyPage pins an 80px header over the top of the viewport.
// Once the page is scrolled so the button sits under the header, the
// button is fully inside the viewport, so a scroll-into-view-if-needed
// does nothing and the click hits the header.
const actionabilityStickyPage = `<!doctype html>
<html>
  <head>
    <title>Sticky</title>
    <style>
      body { margin: 0; }
      header { position: fixed; top: 0; left: 0; right: 0; height: 80px; z-index: 5; background: #234; color: #fff; }
      .spacer-top { height: 1000px; }
      .spacer-bottom { height: 3000px; }
    </style>
  </head>
  <body>
    <header class="topbar" data-testid="sticky.header">Header</header>
    <div class="spacer-top"></div>
    <button data-testid="sticky.submit" type="button">Submit</button>
    <p data-testid="sticky.done" hidden>submitted</p>
    <div class="spacer-bottom"></div>
    <script>
      document.querySelector("[data-testid='sticky.submit']").addEventListener("click", function () {
        document.querySelector("[data-testid='sticky.done']").hidden = false;
      });
    </script>
  </body>
</html>
`

// actionabilityMenuPage opens its menu on CSS :hover only. A synthetic
// mouseover event does not set :hover; a real mouse move does.
const actionabilityMenuPage = `<!doctype html>
<html>
  <head>
    <title>Menu</title>
    <style>
      .menu { display: inline-block; padding: 8px; }
      .menu .items { display: none; margin: 0; }
      .menu:hover .items { display: block; }
    </style>
  </head>
  <body>
    <h1 data-testid="menu.title">Menu</h1>
    <div class="menu" data-testid="menu.trigger">
      Account
      <ul class="items">
        <li><a data-testid="menu.logout" href="#logout">Logout</a></li>
      </ul>
    </div>
    <p data-testid="menu.status">idle</p>
    <script>
      document.querySelector("[data-testid='menu.logout']").addEventListener("click", function (event) {
        event.preventDefault();
        document.querySelector("[data-testid='menu.status']").textContent = "logged out";
      });
    </script>
  </body>
</html>
`

// actionabilityWidgetsPage gathers the targets the hit test must accept:
// a button whose center is one of its own children, a custom element whose
// button lives in an open shadow root, and a styled checkbox whose input is
// covered by its own knob (reachable through its label, or with force).
const actionabilityWidgetsPage = `<!doctype html>
<html>
  <head>
    <title>Widgets</title>
    <style>
      .switch { position: relative; display: inline-block; padding-left: 28px; line-height: 20px; }
      .switch input { position: absolute; left: 0; top: 0; width: 20px; height: 20px; margin: 0; opacity: 0; }
      .switch .knob { position: absolute; left: 0; top: 0; width: 20px; height: 20px; background: #ccc; }
      .switch input:checked + .knob { background: #2a7; }
    </style>
  </head>
  <body>
    <h1 data-testid="widgets.title">Widgets</h1>
    <button data-testid="widgets.nested" type="button"><span class="icon">*</span><span class="caption">Star</span></button>
    <fancy-button data-testid="widgets.shadow"></fancy-button>
    <p>
      <label class="switch" data-testid="widgets.switch.label">
        <input type="checkbox" data-testid="widgets.switch.input" name="notifications" />
        <span class="knob"></span>
        Notifications
      </label>
    </p>
    <p data-testid="widgets.status">idle</p>
    <p data-testid="widgets.changes">0</p>
    <script>
      var statusLine = document.querySelector("[data-testid='widgets.status']");

      document.querySelector("[data-testid='widgets.nested']").addEventListener("click", function () {
        statusLine.textContent = "nested clicked";
      });

      customElements.define("fancy-button", class extends HTMLElement {
        constructor() {
          super();
          var root = this.attachShadow({ mode: "open" });
          root.innerHTML = '<button type="button">Shadow</button>';
          root.querySelector("button").addEventListener("click", function () {
            statusLine.textContent = "shadow clicked";
          });
        }
      });

      var changes = 0;
      document.querySelector("[data-testid='widgets.switch.input']").addEventListener("change", function () {
        changes += 1;
        document.querySelector("[data-testid='widgets.changes']").textContent = String(changes);
      });
    </script>
  </body>
</html>
`

// actionabilityModalPage opens a modal whose backdrop covers the page. A
// click on the backdrop dismisses it, which is what a deliberate
// force = true click on the button behind it exercises.
const actionabilityModalPage = `<!doctype html>
<html>
  <head>
    <title>Modal</title>
    <style>
      .backdrop { position: fixed; inset: 0; z-index: 10; background: rgba(0, 0, 0, 0.4); }
      .dialog { position: absolute; right: 40px; bottom: 40px; width: 240px; padding: 16px; background: #fff; }
    </style>
  </head>
  <body>
    <h1 data-testid="modal.title">Modal</h1>
    <button data-testid="modal.behind" type="button">Behind</button>
    <p data-testid="modal.status">open</p>
    <div class="backdrop" data-testid="modal.backdrop">
      <div class="dialog" role="dialog">Click outside to dismiss</div>
    </div>
    <script>
      var backdrop = document.querySelector("[data-testid='modal.backdrop']");
      var statusLine = document.querySelector("[data-testid='modal.status']");

      document.querySelector("[data-testid='modal.behind']").addEventListener("click", function () {
        statusLine.textContent = "behind clicked";
      });

      backdrop.addEventListener("click", function (event) {
        if (event.target === backdrop) {
          backdrop.remove();
          statusLine.textContent = "dismissed";
        }
      });
    </script>
  </body>
</html>
`

// actionabilityToastPage keeps a toast over its submit button for good. A
// click can never reach the button, so the action must fail and name the
// toast.
const actionabilityToastPage = `<!doctype html>
<html>
  <head>
    <title>Toast</title>
    <style>
      .toast { position: fixed; top: 0; left: 0; width: 400px; height: 200px; z-index: 10; background: #333; color: #fff; }
    </style>
  </head>
  <body>
    <h1 data-testid="toast.title">Toast</h1>
    <button data-testid="toast.submit" type="button">Submit</button>
    <div class="toast" role="status">Saved</div>
  </body>
</html>
`

func writeHTML(w http.ResponseWriter, page string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

// registerActionabilityPages wires every actionability fixture under
// /web/actionability/<name>.
func registerActionabilityPages(handle func(path string, page string)) {
	handle("/web/actionability/veil", actionabilityVeilPage)
	handle("/web/actionability/sliding", actionabilitySlidingPage)
	handle("/web/actionability/sticky", actionabilityStickyPage)
	handle("/web/actionability/menu", actionabilityMenuPage)
	handle("/web/actionability/widgets", actionabilityWidgetsPage)
	handle("/web/actionability/modal", actionabilityModalPage)
	handle("/web/actionability/toast", actionabilityToastPage)
}
