import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { loadFlyModel } from '/fly-static/fly-anatomy.js';
import { createBrainView } from '/fly-static/fly-brain.js';
import { createFlyReaction } from '/fly-static/fly-reactions.js';

const $ = id => document.getElementById(id);
const clamp = (value, low, high) => Math.min(high, Math.max(low, value));
const number = (value, fallback = 0) => Number.isFinite(Number(value)) ? Number(value) : fallback;
const KC_TOTAL = 4133;
const flyReaction = createFlyReaction();
const DEFAULT_TUNE = { pitch: 0.30, lift: 0.38, clapAmp: 1 };
const TUNE_KEY = 'fly-brain-demo-tune';
const state = { pam: 0, ppl1: 0, mode: 'idle', live: true, paused: false, window: 30, reduceMotion: matchMedia('(prefers-reduced-motion: reduce)').matches };
let disposed = false, brainView = null, flyView = null, latestRecommendation = null;
let lastRecKey = '', lastFbKey = '', baselineKnown = false, liveGeneration = 0;
let liveSource = null, pollTimer = null, pollController = null, toastTimer = null, speechTimer = null;
let chartDirty = true, gapNextSample = false, lastChartTime = performance.now() / 1000, frameId = 0;
const failures = new Map(), history = [];
const listeners = new AbortController();
const on = (element, name, callback) => element?.addEventListener(name, callback, { signal: listeners.signal });
const text = (id, value) => { if ($(id)) $(id).textContent = value; };

function reportFailure(key, message) {
  failures.set(key, message);
  const error = $('error');
  if (error) { error.hidden = false; error.textContent = [...failures.values()].join(' '); }
}
function toast(message, kind = 'info') {
  const element = $('event-toast');
  if (!element) return;
  element.textContent = message;
  element.className = `${kind} show`;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => element.classList.remove('show'), 2600);
}
const SPEECH = {
  reward: [
    "I'm the best at this hackathon!",
    "This hackathon is mine!",
    "First place? Obviously.",
    "I'm the best here!",
    "Sweet victory!",
    "I came, I saw, I buzzed.",
    "The other teams can go home.",
    "Judge, blink twice if you're impressed.",
    "Jury, are you taking notes?",
    "My 4133 neurons all say: winner."
  ],
  pain: [
    "I need this job.",
    "I have a family at home.",
    "Please, give me one more chance.",
    "I should work harder.",
    "Don't give up on me.",
    "I need this job more than you need this demo.",
    "Don't fire me — I just learned to walk.",
    "I can't go back to the dumpster."
  ],
  idle: 'I can do better'
};
function speech(reward) {
  const phrases = reward ? SPEECH.reward : SPEECH.pain;
  text('fly-speech', phrases[Math.floor(Math.random() * phrases.length)]);
  $('fly-speech')?.classList.add('show');
  clearTimeout(speechTimer);
  speechTimer = setTimeout(() => text('fly-speech', SPEECH.idle), reward ? 4700 : 7300);
}

const tune = { ...DEFAULT_TUNE };
try {
  const saved = JSON.parse(localStorage.getItem(TUNE_KEY) || 'null');
  if (saved && typeof saved === 'object') for (const key of Object.keys(tune)) {
    if (Number.isFinite(saved[key])) tune[key] = clamp(saved[key], key === 'clapAmp' ? 0 : -1, key === 'clapAmp' ? 2 : 1);
  }
} catch { /* Browser storage is optional. */ }
function syncTune(save = true) {
  for (const [name, key] of [['pitch', 'pitch'], ['lift', 'lift'], ['clap', 'clapAmp']]) {
    const control = $(`t-${name}`);
    if (control) {
      control.value = String(tune[key]);
      control.style.setProperty('--range-progress', `${(tune[key] - Number(control.min)) / (Number(control.max) - Number(control.min)) * 100}%`);
    }
    text(`v-${name}`, tune[key].toFixed(2));
  }
  if (save) try { localStorage.setItem(TUNE_KEY, JSON.stringify(tune)); } catch { /* Browser storage is optional. */ }
}
for (const [name, key] of [['pitch', 'pitch'], ['lift', 'lift'], ['clap', 'clapAmp']]) {
  on($(`t-${name}`), 'input', event => {
    tune[key] = clamp(number(event.target.value, DEFAULT_TUNE[key]), Number(event.target.min), Number(event.target.max));
    syncTune();
  });
}
syncTune(false);

function recommendationStatus() {
  if (brainView) return brainView.getStatus();
  const rec = latestRecommendation;
  const total = Number.isSafeInteger(rec?.kc_total) && rec.kc_total > 0 && rec.kc_total <= 100000 ? rec.kc_total : KC_TOTAL;
  const stamp = typeof rec?.ts === 'number' ? (rec.ts < 1e12 ? rec.ts * 1000 : rec.ts) : Date.parse(rec?.ts);
  const fresh = Number.isFinite(stamp) && Date.now() - stamp >= -1000 && Date.now() - stamp <= 5000;
  const indices = new Set();
  if (fresh && rec?.activation_available === true && Array.isArray(rec.active_kc)) rec.active_kc.forEach(cell => {
    if (Number.isSafeInteger(cell.index) && cell.index >= 0 && cell.index < total && number(cell.activation) > 0) indices.add(cell.index);
  });
  return { kcTotal: total, activeCount: indices.size, fresh, complete: rec?.activation_complete === true, available: rec?.activation_available === true };
}
function sampleValues() {
  const snapshot = recommendationStatus(), kc = snapshot.activeCount || 0, total = snapshot.kcTotal || KC_TOTAL;
  // A normalized display signal; no invented spikes or firing-rate estimate.
  return { pam: state.pam, ppl1: state.ppl1, kc, activity: Math.max(state.pam, state.ppl1, kc / total), total };
}
function updateReadouts() {
  const values = sampleValues(), snapshot = recommendationStatus();
  const partial = snapshot.fresh && snapshot.available && !snapshot.complete;
  text('stat-active', `${partial ? '≥ ' : ''}${values.kc}`);
  text('stat-total', values.total); text('hdr-kc', values.total);
  text('stat-percent', `${partial ? '≥ ' : ''}${(values.kc / values.total * 100).toFixed(1)}%`);
  if ($('kc-progress')) $('kc-progress').style.width = `${clamp(values.kc / values.total * 100, 0, 100)}%`;
  for (const key of ['pam', 'ppl1']) { text(`stat-${key}`, values[key].toFixed(2)); text(`m-${key}`, values[key].toFixed(2)); }
  text('m-activity', values.activity.toFixed(2)); text('m-kc', `${partial ? '≥ ' : ''}${values.kc}`);
  text('brain-status', partial ? 'Частичный снимок активности КС' : state.mode === 'reward' ? 'Ответ на награду · PAM' : state.mode === 'correction' ? 'Ответ на ошибку · PPL1' : 'Карта активности грибовидных тел дрозофилы');
}
function applyRecommendation(rec) {
  if (!rec || typeof rec !== 'object') return;
  latestRecommendation = { ...rec, event_id: rec.event_id || rec.trace_id || null };
  brainView?.update(latestRecommendation);
  updateReadouts();
}
function synthRecommendation() {
  const total = recommendationStatus().kcTotal || KC_TOTAL, count = Math.round(total * 0.06), indices = new Set();
  while (indices.size < count) indices.add(Math.floor(Math.random() * total));
  return { kc_total: total, activation_available: true, activation_complete: true, ts: new Date().toISOString(), event_id: `manual-${Date.now()}`, active_kc: [...indices].map(index => ({ index, activation: 0.3 + Math.random() * 0.7 })) };
}
function fire(kind, { manual = false, strength = 1 } = {}) {
  const reward = kind === 'reward';
  if (manual) applyRecommendation(synthRecommendation());
  // Independent amplitudes preserve simultaneous PAM and PPL1 feedback.
  if (reward) state.pam = clamp(Math.max(state.pam, strength), 0, 1);
  else state.ppl1 = clamp(Math.max(state.ppl1, strength), 0, 1);
  state.mode = kind;
  flyReaction.trigger(kind, performance.now() / 1000);
  const button = $(reward ? 'btn-reward' : 'btn-pain');
  button?.classList.add('flash');
  setTimeout(() => button?.classList.remove('flash'), 500);
  toast(`${manual ? 'Ручная стимуляция' : 'Живое событие'} · ${reward ? 'награда PAM' : 'ошибка PPL1'}`, reward ? 'reward' : 'pain');
  text('status', manual ? 'Ручная симуляция нейронного ответа' : 'Реакция на событие из живого потока');
  speech(reward); updateReadouts();
  if (!state.paused) pushSample(performance.now() / 1000);
}
function applyFeedback(feedback) {
  let reward = null;
  if (typeof feedback.reward === 'boolean') reward = feedback.reward;
  else if (typeof feedback.reward === 'number' && feedback.reward !== 0) reward = feedback.reward > 0;
  else if (['accepted', 'normal-teacher'].includes(feedback.source)) reward = true;
  else if (['correction', 'rejected', 'user-correction', 'teacher-correction', 'codex-correction', 'negative'].includes(feedback.source)) reward = false;
  if (reward === null) return;
  const strength = typeof feedback.reward === 'number' ? clamp(Math.abs(feedback.reward), 0, 1) : 1;
  fire(reward ? 'reward' : 'correction', { strength });
}
function eventKey(record) { return record ? String(record.event_id || record.trace_id || `${record.ts || ''}|${JSON.stringify(record)}`) : ''; }
function setLiveStatus(mode, hasFeed = true) {
  const labels = {
    live: ['В эфире', hasFeed ? 'Подключено к потоку событий' : 'Подключено · ожидание событий'],
    connecting: ['Подключение', 'Подключаем поток событий'],
    polling: ['В эфире', hasFeed ? 'Резервное обновление · каждые 3 с' : 'Ожидание событий · каждые 3 с'],
    offline: ['Нет соединения', 'Ручная стимуляция доступна'], off: ['Поток выключен', 'Доступна ручная стимуляция'],
  };
  const [label, detail] = labels[mode] || labels.offline;
  text('live-pill', label); text('live-detail', detail);
  text('brain-live-label', mode === 'live' || mode === 'polling' ? 'Реальное время' : mode === 'off' ? 'Ручной режим' : 'Ожидание потока');
  $('live-pill')?.classList.toggle('off', mode !== 'live' && mode !== 'polling');
  $('live-pill')?.closest('.live-indicator')?.classList.toggle('off', mode !== 'live' && mode !== 'polling');
}
function consumeFrame(frame, initial) {
  if (!state.live || !frame || typeof frame !== 'object') return;
  const rec = frame.recommendation, feedback = frame.feedback, recId = eventKey(rec), fbId = eventKey(feedback);
  if (rec && recId !== lastRecKey) {
    lastRecKey = recId; applyRecommendation(rec);
    if (!initial) text('status', 'Получен снимок активности КС');
  }
  if (feedback && fbId !== lastFbKey) { lastFbKey = fbId; if (!initial) applyFeedback(feedback); }
  baselineKnown = true;
}
function stopPolling() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = null; pollController?.abort(); pollController = null;
}
async function pollOnce(generation) {
  if (!state.live || disposed || generation !== liveGeneration || pollController) return;
  const controller = new AbortController(); pollController = controller;
  const timeout = setTimeout(() => controller.abort(), 6500);
  try {
    const response = await fetch('/fly-live?snapshot=1', { cache: 'no-store', signal: controller.signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const frame = await response.json();
    if (disposed || !state.live || generation !== liveGeneration || controller.signal.aborted) return;
    setLiveStatus('polling', frame.feed_available !== false);
    // This endpoint marks every poll initial; only the first baseline is history.
    consumeFrame(frame, !baselineKnown);
  } catch (error) {
    if (!controller.signal.aborted && state.live && generation === liveGeneration) setLiveStatus('offline');
  } finally { clearTimeout(timeout); if (pollController === controller) pollController = null; }
}
function startPolling(generation) {
  if (pollTimer) return;
  void pollOnce(generation); pollTimer = setInterval(() => void pollOnce(generation), 3000);
}
function connectLive() {
  const generation = ++liveGeneration;
  liveSource?.close(); liveSource = null; stopPolling();
  if (!state.live || disposed) { setLiveStatus('off'); return; }
  setLiveStatus('connecting');
  if (typeof EventSource === 'undefined') { startPolling(generation); return; }
  const source = new EventSource('/fly-live'); liveSource = source;
  source.addEventListener('snapshot', event => {
    if (disposed || !state.live || source !== liveSource) return;
    let frame; try { frame = JSON.parse(event.data); } catch { return; }
    stopPolling(); setLiveStatus('live', frame.feed_available !== false);
    consumeFrame(frame, frame.initial === true || !baselineKnown);
  });
  source.onopen = () => { if (source === liveSource) { setLiveStatus('live'); stopPolling(); } };
  source.onerror = () => {
    if (source !== liveSource || !state.live || disposed) return;
    setLiveStatus('connecting'); startPolling(generation);
  };
}
function toggleLive() {
  state.live = !state.live;
  $('tgl-live')?.setAttribute('aria-pressed', String(state.live));
  text('live-toggle-text', `Живые события: ${state.live ? 'вкл' : 'выкл'}`);
  if (state.live) baselineKnown = false;
  connectLive();
}
function togglePause() {
  state.paused = !state.paused;
  text('pause-label', state.paused ? 'Продолжить' : 'Пауза');
  $('btn-pause')?.setAttribute('aria-pressed', String(state.paused));
  $('btn-pause')?.classList.toggle('is-paused', state.paused);
  if (!state.paused) { gapNextSample = true; chartDirty = true; }
}
function reset() {
  state.pam = 0; state.ppl1 = 0; state.mode = 'idle'; history.length = 0;
  flyReaction.reset(); flyView?.resetPose();
  gapNextSample = false; lastChartTime = performance.now() / 1000; chartDirty = true;
  Object.assign(tune, DEFAULT_TUNE); syncTune();
  applyRecommendation({ kc_total: recommendationStatus().kcTotal || KC_TOTAL, activation_available: false, activation_complete: true, ts: new Date().toISOString(), active_kc: [] });
  flyView?.setView('side'); if ($('view-select')) $('view-select').value = 'side';
  clearTimeout(speechTimer); text('fly-speech', 'I can do better');
  text('status', 'Поза и активность сброшены'); toast('Активность и графики сброшены'); updateReadouts();
}
on($('btn-reward'), 'click', () => fire('reward', { manual: true }));
on($('btn-pain'), 'click', () => fire('correction', { manual: true }));
on($('btn-reset'), 'click', reset); on($('tgl-live'), 'click', toggleLive); on($('btn-pause'), 'click', togglePause);
on($('window-select'), 'change', event => { state.window = [10, 30, 60].includes(Number(event.target.value)) ? Number(event.target.value) : 30; chartDirty = true; });
on($('btn-settings'), 'click', () => {
  const panel = $('settings-panel'); if (!panel) return;
  panel.hidden = !panel.hidden; $('btn-settings').setAttribute('aria-expanded', String(!panel.hidden));
});
if ($('reduce-motion')) $('reduce-motion').checked = state.reduceMotion;
on($('reduce-motion'), 'change', event => { state.reduceMotion = event.target.checked; });
on(document, 'keydown', event => {
  if (event.repeat || event.altKey || event.ctrlKey || event.metaKey || event.target.closest('input, select, textarea, button, [contenteditable="true"]')) return;
  if (event.key === '1') fire('reward', { manual: true });
  else if (event.key === '2') fire('correction', { manual: true });
  else if (event.key === '3') toggleLive();
  else if (event.key.toLowerCase() === 'r' || event.key.toLowerCase() === 'к') reset();
  else if (event.code === 'Space') { event.preventDefault(); togglePause(); }
});

const chartSpecs = [['chart-activity', 'activity', '#99baff'], ['chart-pam', 'pam', '#35e5f6'], ['chart-ppl1', 'ppl1', '#ef65c7'], ['chart-kc', 'kc', '#48e8b9'], ['spark-pam', 'pam', '#35e5f6', true], ['spark-ppl1', 'ppl1', '#ef65c7', true]];
const charts = chartSpecs.map(([id, key, color, spark]) => {
  const canvas = $(id); return canvas ? { canvas, context: canvas.getContext('2d'), key, color, spark } : null;
}).filter(chart => chart?.context);
function resizeCharts() {
  const dpr = Math.min(window.devicePixelRatio || 1, 2);
  for (const chart of charts) {
    chart.width = Math.max(1, chart.canvas.clientWidth); chart.height = Math.max(1, chart.canvas.clientHeight);
    chart.canvas.width = Math.round(chart.width * dpr); chart.canvas.height = Math.round(chart.height * dpr);
    chart.context.setTransform(dpr, 0, 0, dpr, 0, 0);
  }
  chartDirty = true;
}
function pushSample(time) {
  history.push({ t: time, ...sampleValues(), gap: gapNextSample }); gapNextSample = false;
  while (history.length && history[0].t < time - 65) history.shift();
  lastChartTime = time; chartDirty = true;
}
function drawCharts() {
  for (const chart of charts) {
    const { context: ctx, width: w, height: h, spark, color, key } = chart;
    ctx.clearRect(0, 0, w, h);
    const left = spark ? 2 : 31, top = spark ? 3 : 9, right = w - (spark ? 2 : 9), bottom = h - (spark ? 3 : 23);
    if (right <= left || bottom <= top) continue;
    const maxValue = key === 'kc' ? Math.max(800, Math.ceil(Math.max(0, ...history.filter(item => item.t >= lastChartTime - state.window).map(item => item.kc)) / 200) * 200) : 1;
    if (!spark) {
      ctx.strokeStyle = '#24314a'; ctx.lineWidth = 0.6;
      for (let i = 0; i <= 8; i++) { const x = left + (right - left) * i / 8; ctx.beginPath(); ctx.moveTo(x, top); ctx.lineTo(x, bottom); ctx.stroke(); }
      for (let i = 0; i <= 2; i++) { const y = top + (bottom - top) * i / 2; ctx.beginPath(); ctx.moveTo(left, y); ctx.lineTo(right, y); ctx.stroke(); }
      ctx.font = '10px "Segoe UI", sans-serif'; ctx.fillStyle = '#91a5c9'; ctx.textAlign = 'right';
      ctx.fillText(key === 'kc' ? String(maxValue) : '1.0', left - 8, top + 4); ctx.fillText(key === 'kc' ? '0' : '0.0', left - 8, bottom + 4);
      ctx.textAlign = 'left'; ctx.fillText(`-${state.window}`, left, h - 5); ctx.textAlign = 'right'; ctx.fillText('0', right, h - 5);
      ctx.textAlign = 'center'; ctx.fillText('t (с)', (left + right) / 2, h - 5);
    }
    const window = spark ? 10 : state.window, start = lastChartTime - window, data = history.filter(point => point.t >= start);
    if (!data.length) continue;
    ctx.save(); ctx.beginPath(); ctx.rect(left, top, right - left, bottom - top); ctx.clip();
    const segments = []; let segment = [];
    for (const point of data) {
      if (point.gap && segment.length) { segments.push(segment); segment = []; }
      segment.push({ x: left + clamp((point.t - start) / window, 0, 1) * (right - left), y: bottom - clamp(point[key] / maxValue, 0, 1) * (bottom - top) });
    }
    if (segment.length) segments.push(segment);
    const gradient = ctx.createLinearGradient(0, top, 0, bottom); gradient.addColorStop(0, `${color}24`); gradient.addColorStop(1, `${color}00`);
    for (const points of segments) {
      ctx.beginPath(); ctx.moveTo(points[0].x, bottom); for (const point of points) ctx.lineTo(point.x, point.y);
      ctx.lineTo(points[points.length - 1].x, bottom); ctx.closePath(); ctx.fillStyle = gradient; ctx.fill();
      ctx.beginPath(); points.forEach((point, index) => index ? ctx.lineTo(point.x, point.y) : ctx.moveTo(point.x, point.y));
      ctx.strokeStyle = color; ctx.lineWidth = spark ? 1.6 : 1.45; ctx.lineJoin = 'round'; ctx.lineCap = 'round'; ctx.shadowColor = `${color}55`; ctx.shadowBlur = 5; ctx.stroke();
    }
    ctx.restore();
  }
  chartDirty = false;
}
let chartObserver = null;
if (typeof ResizeObserver !== 'undefined') { chartObserver = new ResizeObserver(resizeCharts); charts.forEach(chart => chartObserver.observe(chart.canvas)); }
else on(window, 'resize', resizeCharts);
resizeCharts();

async function initFly() {
  const stage = $('fly-stage'), scene = new THREE.Scene(), resources = [];
  let renderer, controls, resizeObserver, model;
  function cleanup() { resizeObserver?.disconnect(); controls?.dispose(); model?.dispose(); resources.forEach(resource => resource.dispose()); renderer?.dispose(); renderer?.domElement.remove(); }
  try {
    renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true, powerPreference: 'high-performance' });
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2)); renderer.setClearColor(0x000000, 0);
    renderer.outputColorSpace = THREE.SRGBColorSpace; renderer.toneMapping = THREE.ACESFilmicToneMapping; renderer.toneMappingExposure = 1.0;
    renderer.shadowMap.enabled = true; renderer.shadowMap.type = THREE.PCFSoftShadowMap;
    renderer.domElement.style.cssText = 'display:block;width:100%;height:100%;touch-action:none;outline:none';
    renderer.domElement.setAttribute('aria-label', 'Интерактивная 3D-модель дрозофилы: перетаскивайте для вращения, колесо для масштаба'); renderer.domElement.setAttribute('role', 'img'); stage.appendChild(renderer.domElement);
    const camera = new THREE.PerspectiveCamera(34, 1, 0.01, 80);
    controls = new OrbitControls(camera, renderer.domElement); controls.enableDamping = true; controls.dampingFactor = 0.085;
    controls.minDistance = 2.3; controls.maxDistance = 15; controls.maxPolarAngle = Math.PI * 0.495;
    controls.rotateSpeed = 0.65; controls.zoomSpeed = 0.85; controls.panSpeed = 0.7;
    scene.add(new THREE.HemisphereLight(0xf0e8ff, 0x35456b, 1.8));
    const key = new THREE.DirectionalLight(0xffe3c0, 2.6); key.position.set(2.5, 6, 5); key.castShadow = true;
    key.shadow.mapSize.set(1024, 1024); key.shadow.camera.left = -4; key.shadow.camera.right = 4; key.shadow.camera.top = 4; key.shadow.camera.bottom = -4; key.shadow.normalBias = 0.035; scene.add(key);
    const fill = new THREE.DirectionalLight(0xb1caff, 1.5); fill.position.set(-4, 2, 1); scene.add(fill);
    const rim = new THREE.DirectionalLight(0x9799ff, 2.7); rim.position.set(-2, 3, -4); scene.add(rim);
    const grid = new THREE.GridHelper(18, 36, 0x355991, 0x172a4a); grid.material.transparent = true; grid.material.opacity = 0.32; grid.position.y = -0.026; scene.add(grid); resources.push(grid.geometry, grid.material);
    const ringGeometry = new THREE.RingGeometry(1.48, 1.486, 160), ringMaterial = new THREE.MeshBasicMaterial({ color: 0x558de7, side: THREE.DoubleSide, transparent: true, opacity: 0.63, depthWrite: false });
    const ring = new THREE.Mesh(ringGeometry, ringMaterial); ring.rotation.x = -Math.PI / 2; ring.position.y = -0.014; ring.scale.set(1.23, 1, 1); scene.add(ring); resources.push(ringGeometry, ringMaterial);
    const floorGeometry = new THREE.PlaneGeometry(18, 18), floorMaterial = new THREE.ShadowMaterial({ opacity: 0.24 });
    const floor = new THREE.Mesh(floorGeometry, floorMaterial); floor.rotation.x = -Math.PI / 2; floor.position.y = -0.025; floor.receiveShadow = true; scene.add(floor); resources.push(floorGeometry, floorMaterial);
    model = await loadFlyModel();
    if (disposed) { cleanup(); return; }
    // Warm cuticle keeps anatomy legible against the dark stage.
    const tinted = new Set();
    model.group.traverse(node => {
      if (!node.isMesh) return; const material = node.material;
      if (tinted.has(material) || material.transparent) return; tinted.add(material);
      if (/red/.test(material.name)) material.color.setRGB(0.48, 0.055, 0.065);
      else { material.color.setRGB(0.36, 0.235, 0.145); material.roughness = 0.69; }
    });
    model.group.position.set(0, model.groundOffset || 0.72, 0); scene.add(model.group);
    let selectedView = $('view-select')?.value || 'side', hasSized = false;
    const aim = new THREE.Vector3(0, (model.groundOffset || 0.72) + 0.16, 0);
    function fitDistance() { return Math.max(3.65, 1.50 / (Math.tan(THREE.MathUtils.degToRad(camera.fov / 2)) * camera.aspect)); }
    function setView(view) {
      selectedView = ['side', 'perspective', 'top'].includes(view) ? view : 'side'; controls.target.copy(aim);
      const direction = selectedView === 'top' ? new THREE.Vector3(0, 1, 0.001) : selectedView === 'perspective' ? new THREE.Vector3(0.75, 0.40, 1) : new THREE.Vector3(0.13, 0.20, 1);
      camera.position.copy(direction.normalize().multiplyScalar(fitDistance()).add(aim)); controls.update();
    }
    function resize() {
      const width = Math.max(1, stage.clientWidth), height = Math.max(1, stage.clientHeight), previousFit = fitDistance();
      camera.aspect = width / height; camera.updateProjectionMatrix(); renderer.setSize(width, height, false);
      if (!hasSized) { hasSized = true; setView(selectedView); }
      else { const offset = camera.position.clone().sub(controls.target).multiplyScalar(fitDistance() / previousFit); camera.position.copy(controls.target).add(offset); }
    }
    resize();
    if (typeof ResizeObserver !== 'undefined') { resizeObserver = new ResizeObserver(resize); resizeObserver.observe(stage); } else on(window, 'resize', resize);
    function setTool(pan) {
      controls.mouseButtons.LEFT = pan ? THREE.MOUSE.PAN : THREE.MOUSE.ROTATE; controls.touches.ONE = pan ? THREE.TOUCH.PAN : THREE.TOUCH.ROTATE;
      $('btn-orbit')?.setAttribute('aria-pressed', String(!pan)); $('btn-pan')?.setAttribute('aria-pressed', String(pan));
      $('btn-orbit')?.classList.toggle('active', !pan); $('btn-pan')?.classList.toggle('active', pan); renderer.domElement.style.cursor = pan ? 'move' : 'grab';
    }
    on($('view-select'), 'change', event => setView(event.target.value)); on($('btn-orbit'), 'click', () => setTool(false)); on($('btn-pan'), 'click', () => setTool(true)); on($('btn-fit'), 'click', () => setView(selectedView));
    on($('btn-zoom'), 'click', () => { const offset = camera.position.clone().sub(controls.target); offset.setLength(Math.max(controls.minDistance, offset.length() * 0.8)); camera.position.copy(controls.target).add(offset); controls.update(); });
    on($('btn-fullscreen'), 'click', async () => {
      try { const panel = stage.closest('.scene-card') || stage; if (document.fullscreenElement) await document.exitFullscreen(); else if (panel.requestFullscreen) await panel.requestFullscreen(); else toast('Полноэкранный режим недоступен в этом браузере'); }
      catch { toast('Не удалось открыть полноэкранный режим'); }
    });
    setTool(false); $('stage-load')?.remove(); text('status', 'Перетаскивайте для вращения · колесо для масштаба');

    const poseTarget = new THREE.Vector3();
    let bodyYaw = 0, bodyPitch = 0;
    function resetPose() {
      bodyYaw = 0; bodyPitch = tune.pitch * 0.15;
      model.group.position.set(0, (model.groundOffset || 0.72) + tune.lift * 0.16, 0);
      model.group.rotation.set(0, 0, bodyPitch);
      stage.dataset.reaction = 'idle';
    }
    flyView = { setView, resetPose, dispose: cleanup, render(time, dt) {
      const motion = flyReaction.sample(time, state.reduceMotion);
      const restingHeight = (model.groundOffset || 0.72) + tune.lift * 0.16;
      const damping = 1 - Math.exp(-dt * 11);
      poseTarget.set(motion.x, restingHeight + motion.y * (0.85 + Math.max(0, tune.lift)), motion.z);
      model.group.position.lerp(poseTarget, damping);
      bodyYaw += Math.atan2(Math.sin(motion.yaw - bodyYaw), Math.cos(motion.yaw - bodyYaw)) * damping;
      bodyPitch += (tune.pitch * 0.15 + motion.pitch - bodyPitch) * damping;
      model.group.rotation.set(0, bodyYaw, bodyPitch);
      stage.dataset.reaction = motion.kind;
      const gesture = motion.kind === 'reward' ? 'clap' : motion.kind === 'correction' ? 'offended' : 'idle';
      model.animate(state.reduceMotion ? 0 : time, dt, {
        mode: motion.kind, activity: motion.envelope, pulse: state.pam - state.ppl1,
        gesture, gestureTime: motion.age, gestureStrength: motion.envelope,
        clap: motion.clap * Math.min(1.4, tune.clapAmp), clapAmp: 1,
        speed: state.reduceMotion ? 0 : motion.speed, turn: motion.kind === 'correction' ? 0.3 : 0,
        flight: state.reduceMotion ? 0 : motion.clap * 0.08,
      });
      controls.update(); renderer.render(scene, camera);
    } };

  } catch (error) {
    cleanup(); console.error('Fly model could not load:', error);
    text('stage-load', 'Не удалось загрузить 3D-модель. Обновите страницу, чтобы повторить.');
    reportFailure('fly', 'Модель мухи недоступна. Остальные элементы работают; обновите страницу для повторной загрузки.');
  }
}
async function initBrain() {
  try {
    const view = await createBrainView($('brain-view'), { theme: 'dashboard' });
    if (disposed) { view.dispose(); return; }
    brainView = view; if (latestRecommendation) brainView.update(latestRecommendation);
    $('brain-load')?.remove(); updateReadouts();
  } catch (error) {
    console.error('Brain view could not load:', error); text('brain-load', 'Не удалось загрузить мозг. Обновите страницу для повтора.');
    reportFailure('brain', 'Карта мозга недоступна. События и телеметрия продолжают работать.');
  }
}
function updateClock() {
  const now = new Date(); text('clock-time', now.toLocaleTimeString('ru-RU', { hour12: false }));
  text('clock-date', now.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' }));
}
updateClock(); const clockTimer = setInterval(updateClock, 1000);
let last = performance.now(), sampleAccumulator = 0, readoutAccumulator = 0;
function loop(now) {
  if (disposed) return;
  const dt = clamp((now - last) / 1000, 0, 0.1), elapsed = Math.max(0, (now - last) / 1000); last = now;
  state.pam *= Math.exp(-elapsed * 0.56); state.ppl1 *= Math.exp(-elapsed * 0.66);
  if (state.pam < 0.004) state.pam = 0; if (state.ppl1 < 0.004) state.ppl1 = 0;
  state.mode = !state.pam && !state.ppl1 ? 'idle' : state.pam >= state.ppl1 ? 'reward' : 'correction';
  brainView?.render(now / 1000, dt, { ...state, pulse: state.pam - state.ppl1, activity: Math.max(state.pam, state.ppl1) });
  flyView?.render(now / 1000, dt);
  if (!state.paused) { sampleAccumulator += elapsed; if (sampleAccumulator >= 0.12) { sampleAccumulator = 0; pushSample(now / 1000); } }
  readoutAccumulator += elapsed; if (readoutAccumulator >= 0.1) { readoutAccumulator = 0; updateReadouts(); }
  if (chartDirty) drawCharts(); frameId = requestAnimationFrame(loop);
}
function dispose() {
  if (disposed) return; disposed = true; liveGeneration++; cancelAnimationFrame(frameId); listeners.abort(); liveSource?.close(); stopPolling();
  clearInterval(clockTimer); clearTimeout(toastTimer); clearTimeout(speechTimer); chartObserver?.disconnect(); brainView?.dispose(); flyView?.dispose();
}
addEventListener('pagehide', dispose, { once: true });
addEventListener('pageshow', event => { if (event.persisted && disposed) location.reload(); });
updateReadouts(); connectLive(); void initFly(); void initBrain(); frameId = requestAnimationFrame(loop);
