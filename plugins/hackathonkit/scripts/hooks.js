#!/usr/bin/env node
/*
 * Hackathon Agent Kit Codex hooks.
 *
 * Usage: node hooks.js <session-start|tool-use|stop>
 *
 * Hooks must never break a Codex session: every failure is swallowed and the
 * process exits 0. Session state lives in
 * data/control/sessions.active.jsonl (one JSON object per active session) so
 * overlapping sessions keep their own ids; events are appended to
 * logs/sessions.jsonl and logs/session-events.jsonl.
 */
"use strict";

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");

const EVENT = process.argv[2] || "";

function repoRoot() {
  if (process.env.HACKATHONKIT_REPO_ROOT) {
    return path.resolve(process.env.HACKATHONKIT_REPO_ROOT);
  }
  if (process.env.PLUGIN_ROOT) {
    // Plugin root is <repo>/plugins/hackathonkit.
    return path.resolve(process.env.PLUGIN_ROOT, "..", "..");
  }
  let dir = process.cwd();
  for (let i = 0; i < 8; i += 1) {
    if (
      fs.existsSync(path.join(dir, ".git")) ||
      fs.existsSync(path.join(dir, "configs", "routing.yaml"))
    ) {
      return dir;
    }
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return process.cwd();
}

const ROOT = repoRoot();
const CONTROL_DIR = path.join(ROOT, "data", "control");
const ACTIVE = path.join(CONTROL_DIR, "sessions.active.jsonl");
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
    payload.id,
  ];
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
  return "s-" + stamp + "-" + process.pid;
}

function readActive() {
  try {
    return fs
      .readFileSync(ACTIVE, "utf8")
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        try {
          return JSON.parse(line);
        } catch (_) {
          return null;
        }
      })
      .filter((entry) => entry && typeof entry.id === "string" && entry.id);
  } catch (_) {
    return [];
  }
}

function appendActive(entry) {
  try {
    fs.mkdirSync(CONTROL_DIR, { recursive: true });
    fs.appendFileSync(ACTIVE, JSON.stringify(entry) + "\n");
  } catch (_) {}
}

function removeActive(id) {
  if (!id) return;
  try {
    const kept = readActive().filter((entry) => entry.id !== id);
    const body = kept.map((entry) => JSON.stringify(entry)).join("\n");
    fs.writeFileSync(ACTIVE, kept.length ? body + "\n" : "");
  } catch (_) {}
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
  const bin = path.join(ROOT, "bin", "hackathonkit-mcp");
  try {
    if (fs.existsSync(bin) && fs.statSync(bin).isFile()) {
      return execFileSync(bin, ["--repo-root", ROOT, "--session-context"], {
        timeout: 15000,
        encoding: "utf8",
      });
    }
  } catch (_) {}
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
  return (
    payload.tool_name ||
    payload.tool ||
    payload.name ||
    (payload.tool && typeof payload.tool === "object" && payload.tool.name) ||
    ""
  );
}

switch (EVENT) {
  case "session-start": {
    const payload = parsePayload(readStdin());
    const id = payloadSessionID(payload) || newSessionID();
    appendActive({ id, root: ROOT, started_at: new Date().toISOString() });
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
    const id = payloadSessionID(payload) || lastActiveID();
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

process.exit(0);
