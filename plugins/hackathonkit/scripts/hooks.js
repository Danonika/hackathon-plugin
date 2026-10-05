#!/usr/bin/env node
/*
 * Hackathon Agent Kit Codex hooks.
 *
 * Usage: node hooks.js <session-start|tool-use|stop>
 *
 * Hooks must never break a Codex session: every failure is swallowed and the
 * process exits 0. Each active session owns one file under
 * data/control/sessions/<id>.json (no shared read-modify-write), so
 * overlapping sessions keep their own ids; events are appended to
 * logs/sessions.jsonl and logs/session-events.jsonl.
 */
"use strict";

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

// A session without a stop event is expired after this window.
const SESSION_TTL_MS = 12 * 60 * 60 * 1000;
// Upper bound for a single hook payload. Larger input is truncated with a
// warning instead of being silently mangled.
const STDIN_LIMIT = 8 * 1024 * 1024;

const EVENT = process.argv[2] || "";

function looksLikeRepo(dir) {
  if (!dir) return false;
  try {
    return (
      fs.existsSync(path.join(dir, "configs", "routing.yaml")) ||
      fs.existsSync(path.join(dir, "docker-compose.yml")) ||
      fs.existsSync(path.join(dir, ".git"))
    );
  } catch (_) {
    return false;
  }
}

// Environment-supplied roots are untrusted: require this repo's own marker.
function isHackathonKitRepo(dir) {
  if (!looksLikeRepo(dir)) return false;
  try {
    return fs.existsSync(path.join(dir, "plugins", "hackathonkit", "scripts", "hooks.js"));
  } catch (_) {
    return false;
  }
}

// The binary's containing directory must also be protected: if bin/ is
// group/world-writable another local user can rename the validated file away
// and drop in their own executable. Reject an unsafe owner or mode.
function dirOwnedAndSafe(dir) {
  try {
    const st = fs.statSync(dir);
    if (!st.isDirectory()) return false;
    if (process.platform !== "win32") {
      if (st.mode & 0o022) return false;
      if (typeof process.getuid === "function" && typeof st.uid === "number") {
        const uid = process.getuid();
        if (st.uid !== uid && st.uid !== 0) return false;
      }
    }
    return true;
  } catch (_) {
    return false;
  }
}

// Never execute a binary that another user could replace. Reject any
// group/world-writable mode and, on POSIX, require ownership by the current
// user or root.
function safeBinary(root, name) {
  try {
    const bin = path.join(root, "bin", name);
    if (!dirOwnedAndSafe(path.dirname(bin))) return null;
    const st = fs.statSync(bin);
    if (!st.isFile()) return null;
    // POSIX-only hardening: on Windows ordinary files report mode 0o100666,
    // which would reject every candidate.
    if (process.platform !== "win32") {
      if (st.mode & 0o022) return null;
      if (typeof process.getuid === "function" && typeof st.uid === "number") {
        const uid = process.getuid();
        if (st.uid !== uid && st.uid !== 0) return null;
      }
    }
    return { path: bin, dev: st.dev, ino: st.ino };
  } catch (_) {
    return null;
  }
}

// safeBinary stats the candidate, but execFileSync resolves the path again. A
// re-stat immediately before the exec (matching device/inode) narrows the
// window in which a writable or foreign-owned file could be swapped in.
function binaryStillSafe(candidate) {
  try {
    if (!dirOwnedAndSafe(path.dirname(candidate.path))) return false;
    const st = fs.statSync(candidate.path);
    if (!st.isFile() || st.dev !== candidate.dev || st.ino !== candidate.ino) return false;
    if (process.platform !== "win32" && st.mode & 0o022) return false;
    return true;
  } catch (_) {
    return false;
  }
}

function safeCwd() {
  try {
    return process.cwd();
  } catch (_) {
    return "";
  }
}

function findRepoUp(start) {
  let dir = start;
  for (let i = 0; i < 8; i += 1) {
    // Require the hackathonkit marker, not just any repo, so session state is never
    // written into an unrelated checkout.
    if (isHackathonKitRepo(dir)) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return "";
}

// Resolve the project root from validated candidates: an explicit override,
// then the working directory tree, then the in-repo plugin location. A cached
// plugin install (PLUGIN_ROOT under ~/.codex/plugins/cache) must never win over
// the actual project.
let ROOT_VALIDATED = false;

function repoRoot() {
  const candidates = [];
  if (process.env.HACKATHONKIT_REPO_ROOT) {
    const envRoot = path.resolve(process.env.HACKATHONKIT_REPO_ROOT);
    if (isHackathonKitRepo(envRoot)) candidates.push(envRoot);
  }
  const cwd = safeCwd();
  if (cwd) {
    candidates.push(findRepoUp(cwd));
  }
  if (process.env.PLUGIN_ROOT) {
    const pluginRoot = path.resolve(process.env.PLUGIN_ROOT, "..", "..");
    if (isHackathonKitRepo(pluginRoot)) candidates.push(pluginRoot);
  }
  for (const candidate of candidates) {
    if (isHackathonKitRepo(candidate)) {
      ROOT_VALIDATED = true;
      return candidate;
    }
  }
  // Prefer the repository this hook script lives in over an unrelated cwd.
  const selfRoot = path.resolve(__dirname, "..", "..", "..");
  if (isHackathonKitRepo(selfRoot)) {
    ROOT_VALIDATED = true;
    return selfRoot;
  }
  // Never fall back to an unvalidated plugin-cache path.
  return safeCwd();
}

const ROOT = repoRoot();
// Only a validated repository root may receive session state; an untrusted cwd
// must not be written to.
const ROOT_VALID = ROOT_VALIDATED;
const CONTROL_DIR = path.join(ROOT, "data", "control");
const SESSIONS_DIR = path.join(CONTROL_DIR, "sessions");
const SESSIONS_LOG = path.join(ROOT, "logs", "sessions.jsonl");
const EVENTS_LOG = path.join(ROOT, "logs", "session-events.jsonl");

function appendJSONL(file, record) {
  if (!ROOT_VALID) return;
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.appendFileSync(file, JSON.stringify(record) + "\n");
  } catch (_) {
    /* never fail a session because of logging */
  }
}

function readStdin() {
  try {
    if (process.stdin.isTTY) return "";
    const buf = fs.readFileSync(0);
    // Hook payloads are normally small; only a pathological payload is capped,
    // and that is reported rather than silently cut mid-JSON (which would drop
    // the session id and misattribute the event).
    if (buf.length > STDIN_LIMIT) {
      process.stderr.write("hackathonkit hooks: stdin payload exceeds " + STDIN_LIMIT + " bytes; truncating\n");
      return buf.subarray(0, STDIN_LIMIT).toString("utf8");
    }
    return buf.toString("utf8");
  } catch (_) {
    return "";
  }
}

function parsePayload(text) {
  try {
    const parsed = JSON.parse(text);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch (_) {
    return {};
  }
}

// The hook payload schema varies by harness; accept the common session keys.
function payloadSessionID(payload) {
  const candidates = [
    payload.session_id,
    payload.sessionId,
    payload.conversation_id,
    payload.conversationId,
    payload.thread_id,
    payload.threadId,
  ];
  if (typeof payload.id === "string" && /^s[-_]/.test(payload.id.trim())) {
    // Some harnesses put the session id in `id`, but it is a generic field:
    // only accept session-looking values.
    candidates.push(payload.id);
  }
  if (payload.session && typeof payload.session === "object") {
    candidates.push(payload.session.id);
  }
  if (payload.conversation && typeof payload.conversation === "object") {
    candidates.push(payload.conversation.id);
  }
  for (const candidate of candidates) {
    if (typeof candidate === "string" && candidate.trim()) {
      return candidate.trim();
    }
  }
  return "";
}

function newSessionID() {
  const stamp = new Date().toISOString().replace(/[-:T]/g, "").slice(0, 14);
  const random = crypto.randomBytes(4).toString("hex");
  return "s-" + stamp + "-" + process.pid + "-" + random;
}

// One file per session: concurrent start/stop operations touch different files
// and can never erase each other's state.
function safeStem(id) {
  const raw = String(id);
  const stem =
    raw
      .replace(/[^A-Za-z0-9._-]+/g, "-")
      .replace(/^[.-]+|[.-]+$/g, "")
      .slice(0, 64) || "session";
  // A short hash keeps distinct ids distinct after sanitizing and truncation.
  const hash = crypto.createHash("sha1").update(raw).digest("hex").slice(0, 8);
  return stem + "-" + hash;
}

function sessionFile(id) {
  return path.join(SESSIONS_DIR, safeStem(id) + ".json");
}

function writeActive(entry) {
  if (!ROOT_VALID) return;
  try {
    fs.mkdirSync(SESSIONS_DIR, { recursive: true });
    const now = new Date().toISOString();
    fs.writeFileSync(sessionFile(entry.id), JSON.stringify({ ...entry, updated_at: now }) + "\n");
  } catch (_) {}
}

function ownedByRoot(id) {
  if (!id || !ROOT_VALID) return false;
  try {
    const entry = JSON.parse(fs.readFileSync(sessionFile(id), "utf8"));
    return Boolean(entry && entry.root === ROOT);
  } catch (_) {
    return false;
  }
}

function removeActive(id) {
  if (!id) return;
  try {
    fs.unlinkSync(sessionFile(id));
  } catch (_) {}
}

// Refresh updated_at (and the file mtime) on each tool call so a legitimate
// long-running session is not treated as expired while it is still active.
function touchActive(id) {
  if (!id || !ROOT_VALID) return;
  try {
    const entry = JSON.parse(fs.readFileSync(sessionFile(id), "utf8"));
    if (!entry || entry.root !== ROOT) return;
    writeActive({ ...entry, id: entry.id });
  } catch (_) {}
}

function isExpired(full, entry) {
  try {
    const stat = fs.statSync(full);
    let last = stat.mtimeMs;
    if (entry) {
      const started = Date.parse(entry.started_at || "") || 0;
      const updated = Date.parse(entry.updated_at || "") || 0;
      last = Math.max(last, started, updated);
    }
    return Date.now() - last > SESSION_TTL_MS;
  } catch (_) {
    return false;
  }
}

function readActive() {
  try {
    const entries = [];
    for (const name of fs.readdirSync(SESSIONS_DIR)) {
      if (!name.endsWith(".json")) continue;
      const full = path.join(SESSIONS_DIR, name);
      try {
        const entry = JSON.parse(fs.readFileSync(full, "utf8"));
        if (!entry || typeof entry.id !== "string" || !entry.id) continue;
        const stat = fs.statSync(full);
        if (isExpired(full, entry)) {
          // A session that never emitted stop (crash, timeout) must not keep
          // stealing events; expire it.
          try {
            fs.unlinkSync(full);
          } catch (_) {}
          continue;
        }
        entries.push({ entry, mtime: stat.mtimeMs, started: Date.parse(entry.started_at || "") || stat.mtimeMs });
      } catch (_) {}
    }
    // Oldest first by start time; lastActiveID takes the tail (most recently
    // started). mtime is only a fallback for entries without started_at.
    entries.sort((a, b) => a.started - b.started);
    return entries.map((item) => item.entry);
  } catch (_) {
    return [];
  }
}

function pruneExpiredSessions() {
  if (!ROOT_VALID) return;
  try {
    for (const name of fs.readdirSync(SESSIONS_DIR)) {
      if (!name.endsWith(".json")) continue;
      const full = path.join(SESSIONS_DIR, name);
      try {
        if (isExpired(full)) {
          fs.unlinkSync(full);
        }
      } catch (_) {}
    }
  } catch (_) {}
}

function lastActiveID() {
  const active = readActive();
  return active.length ? active[active.length - 1].id : "";
}

// lastActiveID is derived from ordering, not from the payload; only attribute
// an event to it when it is a session this repository actually owns.
function validatedFallbackID() {
  const id = lastActiveID();
  return id && ownedByRoot(id) ? id : "";
}

function fallbackContext() {
  const lines = ["Hackathon Agent Kit session context", "routing: unknown (run scripts/setup.sh)"];
  try {
    const routing = fs.readFileSync(path.join(ROOT, "configs", "routing.yaml"), "utf8");
    const mode = (routing.match(/mode:\s*(\w+)/) || [])[1];
    const version = (routing.match(/version:\s*(\d+)/) || [])[1];
    if (mode) lines[1] = "routing: " + mode + " (config v" + (version || "?") + ")";
  } catch (_) {}
  return lines.join("\n");
}

function binaryContext() {
  if (!ROOT_VALID) return "";
  const names =
    process.platform === "win32"
      ? ["hackathonkit-mcp.exe", "hackathonkit-mcp"]
      : ["hackathonkit-mcp", "hackathonkit-mcp.exe"];
  for (const name of names) {
    const candidate = safeBinary(ROOT, name);
    if (!candidate || !binaryStillSafe(candidate)) continue;
    try {
      return execFileSync(candidate.path, ["--repo-root", ROOT, "--session-context"], {
        timeout: 15000,
        encoding: "utf8",
      });
    } catch (_) {}
  }
  try {
    const compose = path.join(ROOT, "docker-compose.yml");
    if (fs.existsSync(compose)) {
      // Inside the container the repository is mounted at /workspace, so the
      // host path must not be passed as --repo-root.
      return execFileSync(
        "docker",
        ["compose", "-f", compose, "run", "--rm", "-T", "hackathonkit-mcp", "--session-context"],
        { timeout: 30000, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }
      );
    }
  } catch (_) {}
  return "";
}

function toolName(payload) {
  if (payload.tool && typeof payload.tool === "object" && typeof payload.tool.name === "string") {
    return payload.tool.name;
  }
  for (const candidate of [payload.tool_name, payload.tool, payload.name]) {
    if (typeof candidate === "string" && candidate) return candidate;
  }
  return "";
}

try {
  switch (EVENT) {
    case "session-start": {
      const payload = parsePayload(readStdin());
      const id = payloadSessionID(payload) || newSessionID();
      writeActive({ id, root: ROOT, started_at: new Date().toISOString() });
      appendJSONL(SESSIONS_LOG, {
        ts: new Date().toISOString(),
        type: "session_start",
        session_id: id,
        root: ROOT,
      });
      let context = "";
      try {
        context = binaryContext().trim();
      } catch (_) {}
      if (!context) context = fallbackContext();
      process.stdout.write(context + "\n");
      break;
    }
    case "tool-use": {
      const raw = readStdin();
      const payload = parsePayload(raw);
      let tool = toolName(payload);
      if (!tool) {
        tool = (raw.match(/"tool_name"\s*:\s*"([^"]+)"/) || [])[1] || "";
      }
      const id = payloadSessionID(payload) || validatedFallbackID();
      touchActive(id);
      appendJSONL(EVENTS_LOG, {
        ts: new Date().toISOString(),
        type: "tool_use",
        session_id: id,
        tool: tool || "unknown",
      });
      break;
    }
    case "stop": {
      const payload = parsePayload(readStdin());
      let id = payloadSessionID(payload);
      if (!id) {
        const active = readActive();
        if (active.length === 1) {
          id = active[0].id;
        } else {
          // With zero or several active sessions the payload is the only
          // reliable source; do not remove a session we cannot identify, but
          // prune files that already outlived their TTL.
          pruneExpiredSessions();
          process.stderr.write("hackathonkit hooks: stop without a session id; keeping active sessions\n");
          break;
        }
      }
      if (!ownedByRoot(id)) {
        process.stderr.write("hackathonkit hooks: stop for unknown or foreign session id; ignoring\n");
        break;
      }
      appendJSONL(SESSIONS_LOG, {
        ts: new Date().toISOString(),
        type: "session_end",
        session_id: id,
      });
      removeActive(id);
      break;
    }
    default:
      process.stderr.write("hackathonkit hooks: unknown event " + EVENT + "\n");
  }
} catch (err) {
  // A hook must never fail the harness session: report and exit 0.
  try {
    process.stderr.write("hackathonkit hooks: " + (err && err.message ? err.message : String(err)) + "\n");
  } catch (_) {}
}

// Natural exit lets stdout flush; process.exit() can truncate piped output.
process.exitCode = 0;
