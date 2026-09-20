#!/usr/bin/env node
/*
 * Hackathon Agent Kit Codex hooks.
 *
 * Usage: node hooks.js <session-start|tool-use|stop>
 *
 * Hooks must never break a Codex session: every failure is swallowed and the
 * process exits 0. Session state lives in data/control/session.env (gitignored)
 * and events are appended to logs/sessions.jsonl and logs/session-events.jsonl.
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
const MARKER = path.join(CONTROL_DIR, "session.env");
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

function sessionID() {
  try {
    const text = fs.readFileSync(MARKER, "utf8");
    const match = text.match(/^HACKATHONKIT_SESSION_ID=(.+)$/m);
    if (match) return match[1].trim();
  } catch (_) {}
  return "";
}

function newSessionID() {
  const stamp = new Date().toISOString().replace(/[-:T]/g, "").slice(0, 14);
  return "s-" + stamp + "-" + process.pid;
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
  const args = ["--repo-root", ROOT, "--session-context"];
  const bin = path.join(ROOT, "bin", "hackathonkit-mcp");
  try {
    if (fs.existsSync(bin) && fs.statSync(bin).isFile()) {
      return execFileSync(bin, args, { timeout: 15000, encoding: "utf8" });
    }
  } catch (_) {}
  try {
    const compose = path.join(ROOT, "docker-compose.yml");
    if (fs.existsSync(compose)) {
      return execFileSync(
        "docker",
        ["compose", "-f", compose, "run", "--rm", "-T", "hackathonkit-mcp"].concat(args),
        { timeout: 30000, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }
      );
    }
  } catch (_) {}
  return "";
}

switch (EVENT) {
  case "session-start": {
    const id = newSessionID();
    try {
      fs.mkdirSync(CONTROL_DIR, { recursive: true });
      fs.writeFileSync(
        MARKER,
        "HACKATHONKIT_SESSION_ID=" + id + "\n" +
          "HACKATHONKIT_SESSION_ROOT=" + ROOT + "\n" +
          "HACKATHONKIT_SESSION_STARTED=" + new Date().toISOString() + "\n"
      );
    } catch (_) {}
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
    const payload = readStdin();
    let tool = "";
    try {
      const parsed = JSON.parse(payload);
      tool =
        parsed.tool_name ||
        parsed.tool ||
        parsed.name ||
        (parsed.tool && parsed.tool.name) ||
        "";
    } catch (_) {
      tool = (payload.match(/"tool_name"\s*:\s*"([^"]+)"/) || [])[1] || "";
    }
    appendJSONL(EVENTS_LOG, {
      ts: new Date().toISOString(),
      type: "tool_use",
      session_id: sessionID(),
      tool: tool || "unknown",
    });
    break;
  }
  case "stop": {
    appendJSONL(SESSIONS_LOG, {
      ts: new Date().toISOString(),
      type: "session_end",
      session_id: sessionID(),
    });
    try {
      fs.unlinkSync(MARKER);
    } catch (_) {}
    break;
  }
  default:
    process.stderr.write("hackathonkit hooks: unknown event " + EVENT + "\n");
}

process.exit(0);
