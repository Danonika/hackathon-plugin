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

function findRepoUp(start) {
  let dir = start;
  for (let i = 0; i < 8; i += 1) {
    if (looksLikeRepo(dir)) return dir;
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
function repoRoot() {
  const candidates = [];
  if (process.env.HACKATHONKIT_REPO_ROOT) {
    candidates.push(path.resolve(process.env.HACKATHONKIT_REPO_ROOT));
  }
  candidates.push(findRepoUp(process.cwd()));
  if (process.env.PLUGIN_ROOT) {
    candidates.push(path.resolve(process.env.PLUGIN_ROOT, "..", ".."));
  }
  for (const candidate of candidates) {
    if (looksLikeRepo(candidate)) return candidate;
  }
  // Never fall back to an unvalidated plugin-cache path.
  return process.cwd();
}

const ROOT = repoRoot();
const CONTROL_DIR = path.join(ROOT, "data", "control");
const SESSIONS_DIR = path.join(CONTROL_DIR, "sessions");
const SESSIONS_LOG = path.join(ROOT, "logs", "sessions.jsonl");
const EVENTS_LOG = path.join(ROOT, "logs", "session-events.jsonl");

function appendJSONL(file, record) {
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
    return fs.readFileSync(0, "utf8").slice(0, 8192);
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
  try {
    fs.mkdirSync(SESSIONS_DIR, { recursive: true });
    fs.writeFileSync(sessionFile(entry.id), JSON.stringify(entry) + "\n");
  } catch (_) {}
}

function removeActive(id) {
  if (!id) return;
  try {
    fs.unlinkSync(sessionFile(id));
  } catch (_) {}
}

function readActive() {
  try {
    const entries = [];
    const now = Date.now();
    for (const name of fs.readdirSync(SESSIONS_DIR)) {
      if (!name.endsWith(".json")) continue;
      const full = path.join(SESSIONS_DIR, name);
      try {
        const entry = JSON.parse(fs.readFileSync(full, "utf8"));
        if (!entry || typeof entry.id !== "string" || !entry.id) continue;
        const stat = fs.statSync(full);
        const started = Date.parse(entry.started_at || "") || stat.mtimeMs;
        if (now - started > SESSION_TTL_MS) {
          // A session that never emitted stop (crash, timeout) must not keep
          // stealing events; expire it.
          try {
            fs.unlinkSync(full);
          } catch (_) {}
          continue;
        }
        entries.push({ entry, mtime: stat.mtimeMs });
      } catch (_) {}
    }
    entries.sort((a, b) => {
      const byStart = String(a.entry.started_at || "").localeCompare(String(b.entry.started_at || ""));
      return byStart !== 0 ? byStart : a.mtime - b.mtime;
    });
    return entries.map((item) => item.entry);
  } catch (_) {
    return [];
  }
}

function lastActiveID() {
  const active = readActive();
  return active.length ? active[active.length - 1].id : "";
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
  const names =
    process.platform === "win32"
      ? ["hackathonkit-mcp.exe", "hackathonkit-mcp"]
      : ["hackathonkit-mcp", "hackathonkit-mcp.exe"];
  for (const name of names) {
    const bin = path.join(ROOT, "bin", name);
    try {
      if (fs.existsSync(bin) && fs.statSync(bin).isFile()) {
        return execFileSync(bin, ["--repo-root", ROOT, "--session-context"], {
          timeout: 15000,
          encoding: "utf8",
        });
      }
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
    const id = payloadSessionID(payload) || lastActiveID();
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
        // reliable source; do not remove a session we cannot identify.
        process.stderr.write("hackathonkit hooks: stop without a session id; keeping active sessions\n");
        break;
      }
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

// Natural exit lets stdout flush; process.exit() can truncate piped output.
process.exitCode = 0;
