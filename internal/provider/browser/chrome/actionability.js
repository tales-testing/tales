// Actionability probe for the pointer actions (click, hover, check,
// uncheck). Evaluated with awaitPromise by actionability.go as
// `(<this function>)(selector, options)`; it answers one JSON object:
//
//   { ready: true, x, y }                   the mouse can go to (x, y)
//   { reason: "<code>", x?, y?, hit? }      not yet; retried until the deadline
//   { fatal: "<message>" }                  never will be, e.g. a bad selector
//
// options: { force, requireEnabled, scrollAttempt }. With force, only the
// presence of the element and a point inside the viewport are required.
// x and y are CSS pixels from the top-left corner of the viewport, the
// space Input.dispatchMouseEvent takes.
(async function (selector, options) {
  var element;

  try {
    element = document.querySelector(selector);
  } catch (error) {
    return { fatal: "invalid selector: " + error.message };
  }

  if (!element) {
    return { reason: "not_found" };
  }

  // One rendered frame. A page that does not render (a hidden tab) never
  // runs requestAnimationFrame, so a timer stands in for it.
  function nextFrame() {
    return new Promise(function (resolve) {
      var settled = false;
      var settle = function () {
        if (!settled) {
          settled = true;
          resolve();
        }
      };

      requestAnimationFrame(settle);
      setTimeout(settle, 100);
    });
  }

  function isVisible(node) {
    if (typeof node.checkVisibility === "function" && !node.checkVisibility()) {
      return false;
    }

    if (getComputedStyle(node).visibility !== "visible") {
      return false;
    }

    var rect = node.getBoundingClientRect();

    return rect.width > 0 && rect.height > 0;
  }

  // A native form control that is disabled (directly, or through a
  // disabled fieldset) swallows the click, and so does any child of a
  // disabled button. aria-disabled does not stop events, so it is not
  // treated as disabled.
  function isDisabled(node) {
    if (node.localName !== "fieldset" && node.matches(":disabled")) {
      return true;
    }

    var button = node.parentElement && node.parentElement.closest("button");

    return !!button && button.matches(":disabled");
  }

  function viewport() {
    var root = document.documentElement;

    if (document.compatMode === "CSS1Compat" && root) {
      return { width: root.clientWidth, height: root.clientHeight };
    }

    return { width: innerWidth, height: innerHeight };
  }

  function sameRect(a, b) {
    return a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;
  }

  // The parent in the composed tree: a shadow root leads to its host.
  function composedParent(node) {
    var parent = node.parentNode;

    if (parent && parent.nodeType === Node.DOCUMENT_FRAGMENT_NODE && parent.host) {
      return parent.host;
    }

    return parent;
  }

  function composedContains(root, node) {
    for (var current = node; current; current = composedParent(current)) {
      if (current === root) {
        return true;
      }
    }

    return false;
  }

  // elementFromPoint stops at a shadow host; follow open shadow roots
  // down to the element the event would really target.
  function hitTarget(x, y) {
    var hit = document.elementFromPoint(x, y);

    while (hit && hit.shadowRoot) {
      var inner = hit.shadowRoot.elementFromPoint(x, y);

      if (!inner || inner === hit) {
        break;
      }

      hit = inner;
    }

    return hit;
  }

  function truncate(value, max) {
    return value.length > max ? value.slice(0, max - 1) + "…" : value;
  }

  // <div class="toast" role="status">: the tag plus the attributes that
  // tell elements apart, at most three of them.
  function preview(node) {
    var parts = [node.localName];
    var names = ["id", "class", "data-testid", "role", "name", "type", "aria-label"];

    for (var i = 0; i < names.length && parts.length < 4; i++) {
      var value = node.getAttribute(names[i]);

      if (value !== null && value !== "") {
        parts.push(names[i] + '="' + truncate(value, 40) + '"');
      }
    }

    return "<" + parts.join(" ") + ">";
  }

  // Names the element that took the point. When it sits inside a larger
  // overlay (a <span> inside a toast), the overlay's root is named too.
  function describeHit(hit) {
    var root = hit;

    for (var current = composedParent(hit); current && current.nodeType === Node.ELEMENT_NODE; current = composedParent(current)) {
      if (composedContains(current, element)) {
        break;
      }

      root = current;
    }

    if (root === hit) {
      return preview(hit);
    }

    return preview(hit) + " from " + preview(root) + " subtree";
  }

  // The center of the largest part of the element inside the viewport.
  // An inline element spanning two lines has one client rect per line,
  // and the center of its bounding box can fall between them. boxed tells
  // an element with no box at all (display: none) from one that has a box
  // outside the viewport.
  function clickPoint(vp) {
    var rects = element.getClientRects();
    var best = null;
    var bestArea = 0;
    var boxed = false;

    for (var i = 0; i < rects.length; i++) {
      if (rects[i].width > 0 && rects[i].height > 0) {
        boxed = true;
      }

      var left = Math.max(rects[i].left, 0);
      var top = Math.max(rects[i].top, 0);
      var right = Math.min(rects[i].right, vp.width);
      var bottom = Math.min(rects[i].bottom, vp.height);

      if (right <= left || bottom <= top) {
        continue;
      }

      var area = (right - left) * (bottom - top);

      if (area > bestArea) {
        bestArea = area;
        best = { x: (left + right) / 2, y: (top + bottom) / 2 };
      }
    }

    return { point: best, boxed: boxed };
  }

  if (!options.force) {
    if (!isVisible(element)) {
      return { reason: "not_visible" };
    }

    if (options.requireEnabled && isDisabled(element)) {
      return { reason: "disabled" };
    }
  }

  // Scroll into view if needed, centered as DOM.scrollIntoViewIfNeeded
  // does. When the previous probes found the point covered (a sticky
  // header over an element that is already inside the viewport), try
  // other alignments in turn to get it out from under the cover.
  var alignments = [null, "end", "center", "start"];
  var alignment = alignments[options.scrollAttempt % alignments.length];
  var vp = viewport();

  if (alignment) {
    element.scrollIntoView({ block: alignment, inline: alignment, behavior: "instant" });
  } else {
    var box = element.getBoundingClientRect();

    if (box.top < 0 || box.left < 0 || box.bottom > vp.height || box.right > vp.width) {
      element.scrollIntoView({ block: "center", inline: "center", behavior: "instant" });
    }
  }

  if (!options.force) {
    // Stable: the same box on two consecutive animation frames.
    await nextFrame();
    var before = element.getBoundingClientRect();
    await nextFrame();
    var after = element.getBoundingClientRect();

    if (!element.isConnected) {
      return { reason: "not_found" };
    }

    if (!sameRect(before, after)) {
      return { reason: "not_stable" };
    }
  }

  var target = clickPoint(viewport());
  var point = target.point;

  if (!point) {
    return { reason: target.boxed ? "outside_viewport" : "no_box" };
  }

  if (!options.force) {
    var hit = hitTarget(point.x, point.y);

    if (!hit) {
      return { reason: "outside_viewport" };
    }

    if (!composedContains(element, hit)) {
      var ancestor = composedContains(hit, element);

      return {
        reason: ancestor ? "hits_ancestor" : "covered",
        hit: describeHit(hit),
        x: point.x,
        y: point.y
      };
    }
  }

  return { ready: true, x: point.x, y: point.y };
})
