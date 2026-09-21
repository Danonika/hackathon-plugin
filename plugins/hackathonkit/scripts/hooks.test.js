#!/usr/bin/env node
/*
 * Hook behaviour test: overlapping sessions must keep their own ids.
 *
 * Usage: node hooks.test.js
 * Exit code 0 when every assertion passes, 1 otherwise.
 */
"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFileSync } = require("child_process");

const HOOKS = path.join(__dirname, "hooks.js");
const root = fs.mkdtempSync(path.join(os.tmpdir(), "hackathonkit-hooks-"));
fs.mkdirSync(path.join(root, "configs"), { recursive: true });
fs.writeFileSync(path.join(root, "configs", "routing.yaml"), "routing:\n  mode: fly\n  version: 7\n");

let failures = 0;
function check(name, condition, detail) {
  if (condition) {
    console.log("[ok]   " + name);
    return;
  }
  console.log("[fail] " + name + (detail ? ": " + detail : ""));
  failures += 1;
}

function run(event, payload) {
  const input = payload === undefined ? "" : JSON.stringify(payload);
  try {
    return execFileSync("node", [HOOKS, event], {
      input,
      encoding: "utf8",
      env: { ...process.env, HACKATHONKIT_REPO_ROOT: root },
    });
  } catch (err) {
    return (err.stdout || "") + (err.stderr || "");
  }
}

function active() {
  try {
    return fs
      .readFileSync(path.join(root, "data", "control", "sessions.active.jsonl"), "utf8")
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } catch (_) {
    return [];
  }
}

function events(file) {
  try {
    return fs
      .readFileSync(path.join(root, "logs", file), "utf8")
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } catch (_) {
    return [];
  }
}

// A starts, then B starts while A is still active.
const startA = run("session-start", { session_id: "A" });
check("session-start A exits cleanly", startA.includes("Hackathon Agent Kit session context"), startA.slice(0, 80));
run("session-start", { session_id: "B" });
check("both sessions are registered", active().length === 2, JSON.stringify(active()));

// Events with an explicit payload id go to that session, even when B is newest.
run("tool-use", { session_id: "A", tool_name: "alpha" });
const alpha = events("session-events.jsonl").find((e) => e.tool === "alpha");
check("explicit session id owns its event", alpha && alpha.session_id === "A", JSON.stringify(alpha));

// Events without an id fall back to the most recently started active session.
run("tool-use", { tool_name: "beta" });
const beta = events("session-events.jsonl").find((e) => e.tool === "beta");
check("fallback attributes to the newest session", beta && beta.session_id === "B", JSON.stringify(beta));

// Stopping A removes only A.
run("stop", { session_id: "A" });
const remaining = active().map((entry) => entry.id);
check("stop removes only its own session", remaining.length === 1 && remaining[0] === "B", JSON.stringify(remaining));
const ended = events("sessions.jsonl").find((e) => e.type === "session_end" && e.session_id === "A");
check("session_end recorded for A", Boolean(ended), JSON.stringify(ended));

// B keeps receiving events after A stopped.
run("tool-use", { tool_name: "gamma" });
const gamma = events("session-events.jsonl").find((e) => e.tool === "gamma");
check("B keeps ownership after A stops", gamma && gamma.session_id === "B", JSON.stringify(gamma));

fs.rmSync(root, { recursive: true, force: true });
if (failures > 0) {
  console.log("[hooks-test] " + failures + " assertion(s) failed");
  process.exit(1);
}
console.log("[hooks-test] all assertions passed");
