export const ACTIVE_WINDOW = 5000;
export const SIGNAL_WINDOW = 4200;

export function createLiveState() {
  return { initialized: false, recommendation: null, feedback: null, recommendationId: '', feedbackId: '',
    connection: 'connecting', mode: 'idle', signalAt: 0, activity: 0, pulse: 0, activeCount: 0, total: 4133,
    complete: false, available: false, receivedAt: 0, feedAvailable: false };
}
const identity = e => e ? (e.event_id || `${e.type}:${e.trace_id || ''}:${e.ts || ''}`) : '';
const timestamp = e => Number.isFinite(Date.parse(e?.ts)) ? Date.parse(e.ts) : 0;
const fresh = (e, now, window) => timestamp(e) > 0 && now - timestamp(e) >= -2000 && now - timestamp(e) < window;

export function applySnapshot(state, snapshot, now = Date.now()) {
  if (!snapshot || snapshot.schema_version !== 1) return false;
  const reco = snapshot.recommendation || null, feedback = snapshot.feedback || null;
  const recoId = identity(reco), feedbackId = identity(feedback);
  const newReco = recoId && recoId !== state.recommendationId;
  const newFeedback = feedbackId && feedbackId !== state.feedbackId;
  const signals = [];
  // First connection and reconnects restore history without replaying rewards.
  if (state.initialized && newReco && fresh(reco, now, SIGNAL_WINDOW)) signals.push({type:'routing', at:timestamp(reco)});
  if (state.initialized && newFeedback && fresh(feedback, now, SIGNAL_WINDOW)) {
    signals.push({type: feedback.reward ? 'reward' : 'correction', at:timestamp(feedback)});
  }
  if (signals.length) {
    signals.sort((a,b) => a.at-b.at);
    state.mode = signals.at(-1).type;
    state.signalAt = signals.at(-1).at;
  }
  state.recommendation = reco;
  state.feedback = feedback;
  state.recommendationId = recoId;
  state.feedbackId = feedbackId;
  state.total = Number.isInteger(reco?.kc_total) && reco.kc_total > 0 ? reco.kc_total : 4133;
  const active = new Map();
  for (const cell of reco?.active_kc || []) {
    if (Number.isInteger(cell.index) && cell.index >= 0 && cell.index < state.total && Number.isFinite(cell.activation) && cell.activation > 0) active.set(cell.index, cell.activation);
  }
  state.activeCount = active.size;
  state.complete = reco?.activation_complete === true;
  state.available = reco?.activation_available === true || (reco?.activation_available == null && active.size > 0);
  state.feedAvailable = !!snapshot.feed_available;
  state.receivedAt = now;
  state.initialized = true;
  return !!(newReco || newFeedback);
}

export function advanceVisualState(state, now, dt) {
  const age = now-state.signalAt;
  if (age >= SIGNAL_WINDOW || age < -2000) state.mode='idle';
  const target = state.available && fresh(state.recommendation, now, ACTIVE_WINDOW)
    ? Math.min(1, state.activeCount / Math.max(1, state.total * 0.12)) : 0;
  state.activity += (target-state.activity) * (1-Math.exp(-Math.max(0,dt)*7));
  state.pulse = state.mode === 'idle' ? 0 : Math.max(0,1-Math.max(0,age)/SIGNAL_WINDOW);
  return state;
}

export function activeNow(state, now=Date.now()) {
  return state.available && fresh(state.recommendation,now,ACTIVE_WINDOW);
}
