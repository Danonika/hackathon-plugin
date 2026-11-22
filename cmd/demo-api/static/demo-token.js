// Both dashboards share this session token. The input still works when storage is disabled.
function bindDemoToken(field) {
  const key = "hackathonkitToken";
  try { field.value = sessionStorage.getItem(key) || ""; } catch {}
  return function tokenHeader() {
    const token = field.value.trim();
    try {
      if (token) sessionStorage.setItem(key, token);
      else sessionStorage.removeItem(key);
    } catch {}
    return token ? { "X-HackathonKit-Token": token } : {};
  };
}
