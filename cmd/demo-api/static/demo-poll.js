// Keep each dashboard snapshot ordered and retry after a slow or failed request.
async function fetchSnapshotJSON(url) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 8000);
  try {
    const response = await fetch(url, {signal: controller.signal, cache: "no-store"});
    if (!response.ok) throw new Error(url + " returned HTTP " + response.status);
    return await response.json();
  } finally {
    clearTimeout(timer);
  }
}

function pollSnapshots(load, delay) {
  async function next() {
    try { await load(); }
    finally { setTimeout(next, delay); }
  }
  void next();
}
