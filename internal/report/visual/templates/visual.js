'use strict';

(function () {
  var dataEl = document.getElementById('tales-report-data');
  if (!dataEl) {
    return;
  }

  var report;
  try {
    report = JSON.parse(dataEl.textContent || '{}');
  } catch (e) {
    console.error('tales: failed to parse report data', e);
    return;
  }

  var scenarios = Array.isArray(report.scenarios) ? report.scenarios : [];
  if (scenarios.length === 0 || !hasAnyAction(scenarios)) {
    showEmptyState();
    return;
  }

  var MIN_DURATION_MS = 500;
  var MAX_DURATION_MS = 3000;

  var state = {
    scenarioIndex: 0,
    stepIndex: 0,
    actionIndex: 0,
    playing: false,
    speed: 1,
    timer: null
  };

  var $ = function (id) { return document.getElementById(id); };

  var els = {
    scenarioSelect: $('scenario-select'),
    stepSelect: $('step-select'),
    overallStatus: $('overall-status'),
    screenshot: $('screenshot'),
    placeholder: $('screenshot-placeholder'),
    timeline: $('timeline'),
    meta: $('action-meta'),
    prev: $('prev'),
    next: $('next'),
    play: $('play'),
    progress: $('progress'),
    speed: $('speed')
  };

  init();

  function init() {
    renderHeader();
    populateScenarios();
    populateSteps();
    renderTimeline();
    renderMeta();
    wireControls();
    document.addEventListener('keydown', onKey);
  }

  function hasAnyAction(list) {
    for (var i = 0; i < list.length; i++) {
      var steps = list[i].steps || [];
      for (var j = 0; j < steps.length; j++) {
        if ((steps[j].actions || []).length > 0) {
          return true;
        }
      }
    }
    return false;
  }

  function showEmptyState() {
    var card = document.getElementById('card');
    if (card) {
      card.hidden = true;
    }
    var empty = document.getElementById('empty-state');
    if (empty) {
      empty.hidden = false;
    }
  }

  function renderHeader() {
    var status = report.status || 'unknown';
    els.overallStatus.textContent = status;
    els.overallStatus.className = 'status-pill ' + status;
  }

  function populateScenarios() {
    els.scenarioSelect.innerHTML = '';
    for (var i = 0; i < scenarios.length; i++) {
      var opt = document.createElement('option');
      opt.value = String(i);
      opt.textContent = scenarios[i].name + ' — ' + scenarios[i].status;
      els.scenarioSelect.appendChild(opt);
    }
    els.scenarioSelect.value = String(state.scenarioIndex);
    els.scenarioSelect.addEventListener('change', function (e) {
      state.scenarioIndex = parseInt(e.target.value, 10) || 0;
      state.stepIndex = 0;
      state.actionIndex = 0;
      populateSteps();
      renderTimeline();
      renderMeta();
    });
  }

  function populateSteps() {
    els.stepSelect.innerHTML = '';
    var sc = currentScenario();
    var steps = sc && sc.steps ? sc.steps : [];
    for (var i = 0; i < steps.length; i++) {
      var opt = document.createElement('option');
      opt.value = String(i);
      opt.textContent = steps[i].name + ' — ' + steps[i].status;
      els.stepSelect.appendChild(opt);
    }
    els.stepSelect.value = String(state.stepIndex);
    els.stepSelect.addEventListener('change', function (e) {
      state.stepIndex = parseInt(e.target.value, 10) || 0;
      state.actionIndex = 0;
      renderTimeline();
      renderMeta();
    });
  }

  function currentScenario() {
    return scenarios[state.scenarioIndex];
  }

  function currentStep() {
    var sc = currentScenario();
    if (!sc || !sc.steps) {
      return null;
    }
    return sc.steps[state.stepIndex] || null;
  }

  function currentActions() {
    var st = currentStep();
    return st && st.actions ? st.actions : [];
  }

  function currentAction() {
    return currentActions()[state.actionIndex] || null;
  }

  function renderTimeline() {
    els.timeline.innerHTML = '';
    var actions = currentActions();
    for (var i = 0; i < actions.length; i++) {
      els.timeline.appendChild(renderActionRow(actions[i], i));
    }
    updateActiveRow();
    renderScreenshot();
    updateProgress();
  }

  function renderActionRow(action, idx) {
    var li = document.createElement('li');
    li.className = 'action-row ' + (action.status || '');
    li.dataset.index = String(idx);
    li.setAttribute('role', 'option');
    li.tabIndex = 0;

    var index = document.createElement('div');
    index.className = 'index';
    index.textContent = String(action.index + 1).padStart(2, '0');
    li.appendChild(index);

    var body = document.createElement('div');
    body.className = 'body';
    var label = document.createElement('div');
    label.className = 'label';
    label.textContent = action.label || action.kind;
    var sub = document.createElement('div');
    sub.className = 'sub';
    sub.textContent = formatDuration(action.duration_ms) + (action.selector_id ? ' · ' + action.selector_id : '');
    body.appendChild(label);
    body.appendChild(sub);
    li.appendChild(body);

    var status = document.createElement('div');
    status.className = 'status';
    li.appendChild(status);

    li.addEventListener('click', function () {
      selectAction(idx);
    });
    li.addEventListener('keydown', function (e) {
      if (e.key === 'Enter') {
        selectAction(idx);
      }
    });

    return li;
  }

  function selectAction(idx) {
    var actions = currentActions();
    if (idx < 0 || idx >= actions.length) {
      return;
    }
    state.actionIndex = idx;
    updateActiveRow();
    renderScreenshot();
    renderMeta();
    updateProgress();
  }

  function updateActiveRow() {
    var rows = els.timeline.querySelectorAll('.action-row');
    for (var i = 0; i < rows.length; i++) {
      rows[i].classList.toggle('active', i === state.actionIndex);
    }
    var active = rows[state.actionIndex];
    if (active && active.scrollIntoView) {
      active.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }
  }

  function renderScreenshot() {
    var action = currentAction();
    if (action && action.screenshot) {
      els.screenshot.src = action.screenshot;
      els.screenshot.alt = action.label || action.kind;
      els.placeholder.hidden = true;
    } else {
      els.screenshot.removeAttribute('src');
      els.placeholder.hidden = false;
    }
  }

  function renderMeta() {
    var action = currentAction();
    els.meta.innerHTML = '';
    if (!action) {
      return;
    }

    var fields = [
      ['Kind', action.kind],
      ['Label', action.label],
      ['Selector', action.selector_id || '—'],
      ['Status', action.status],
      ['Duration', formatDuration(action.duration_ms)]
    ];

    if (action.value) {
      fields.push(['Value', action.secure ? '***' : action.value]);
    }

    for (var i = 0; i < fields.length; i++) {
      var f = document.createElement('div');
      f.className = 'meta-field';
      var k = document.createElement('span');
      k.textContent = fields[i][0];
      var v = document.createElement('span');
      v.textContent = fields[i][1];
      f.appendChild(k);
      f.appendChild(v);
      els.meta.appendChild(f);
    }

    if (action.error) {
      var err = document.createElement('div');
      err.className = 'error';
      err.textContent = action.error;
      els.meta.appendChild(err);
    }
  }

  function wireControls() {
    els.prev.addEventListener('click', function () { selectAction(state.actionIndex - 1); });
    els.next.addEventListener('click', function () { selectAction(state.actionIndex + 1); });
    els.play.addEventListener('click', togglePlay);

    var speedButtons = els.speed.querySelectorAll('button');
    for (var i = 0; i < speedButtons.length; i++) {
      speedButtons[i].addEventListener('click', onSpeedClick);
    }
  }

  function onSpeedClick(e) {
    var btn = e.target;
    state.speed = parseFloat(btn.dataset.speed) || 1;
    var siblings = els.speed.querySelectorAll('button');
    for (var i = 0; i < siblings.length; i++) {
      siblings[i].classList.toggle('active', siblings[i] === btn);
    }
  }

  function togglePlay() {
    if (state.playing) {
      pause();
    } else {
      play();
    }
  }

  function play() {
    state.playing = true;
    els.play.textContent = '❚❚';
    schedule();
  }

  function pause() {
    state.playing = false;
    els.play.textContent = '▶';
    if (state.timer) {
      clearTimeout(state.timer);
      state.timer = null;
    }
  }

  function schedule() {
    if (!state.playing) {
      return;
    }
    var action = currentAction();
    if (!action) {
      pause();
      return;
    }
    var delay = durationFor(action);
    state.timer = setTimeout(function () {
      var actions = currentActions();
      if (state.actionIndex + 1 >= actions.length) {
        pause();
        return;
      }
      selectAction(state.actionIndex + 1);
      schedule();
    }, delay);
  }

  function durationFor(action) {
    var raw = Math.max(MIN_DURATION_MS, Math.min(MAX_DURATION_MS, action.duration_ms || MIN_DURATION_MS));
    return Math.max(50, raw / state.speed);
  }

  function updateProgress() {
    var actions = currentActions();
    if (actions.length === 0) {
      els.progress.value = 0;
      return;
    }
    els.progress.value = Math.round(((state.actionIndex + 1) / actions.length) * 100);
  }

  function formatDuration(ms) {
    if (!ms || ms < 0) {
      return '0ms';
    }
    if (ms < 1000) {
      return ms + 'ms';
    }
    return (ms / 1000).toFixed(2) + 's';
  }

  function onKey(e) {
    if (e.target && (e.target.tagName === 'SELECT' || e.target.tagName === 'INPUT')) {
      return;
    }
    switch (e.key) {
      case ' ': e.preventDefault(); togglePlay(); break;
      case 'ArrowRight': selectAction(state.actionIndex + 1); break;
      case 'ArrowLeft': selectAction(state.actionIndex - 1); break;
      case 'Home': selectAction(0); break;
      case 'End': selectAction(currentActions().length - 1); break;
    }
  }
})();
