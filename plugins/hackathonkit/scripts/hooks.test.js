#!/usr/bin/env node
/*
 * Hook behaviour tests.
 *
 * Usage: node hooks.test.js
 * Exit code 0 when every assertion passes, 1 otherwise.
 *
 * Covers: overlapping session ownership, cached plugin roots, per-session
 * state files (no shared read-modify-write), unsafe session ids, and the
 * Windows .exe binary lookup.
 */
"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const { execFileSync } = require("child_process");

const HOOKS = path.join(__dirname, "hooks.js");
// A fixture is only a hackathonkit repo when it carries the plugin marker
// (hooks.js), matching isHackathonKitRepo in hooks.js.
function markHackathonKitRepo(dir) {
  const scripts = path.join(dir, "plugins", "hackathonkit", "scripts");
  fs.mkdirSync(scripts, { recursive: true });
  fs.writeFileSync(path.join(scripts, "hooks.js"), "// hackathonkit marker fixture\n");
}

const root = fs.mkdtempSync(path.join(os.tmpdir(), "hackathonkit-hooks-"));
fs.mkdirSync(path.join(root, "configs"), { recursive: true });
fs.writeFileSync(path.join(root, "configs", "routing.yaml"), "routing:\n  mode: fly\n  version: 7\n");
markHackathonKitRepo(root);

let failures = 0;
function check(name, condition, detail) {
  if (condition) {
    console.log("[ok]   " + name);
    return;
  }
  console.log("[fail] " + name + (detail ? ": " + detail : ""));
  failures += 1;
}

function run(event, payload, options = {}) {
  const env = { ...process.env, HACKATHONKIT_REPO_ROOT: root };
  for (const [key, value] of Object.entries(options.env || {})) {
    if (value === undefined) delete env[key];
    else env[key] = value;
  }
  const input = payload === undefined ? "" : JSON.stringify(payload);
  try {
    return execFileSync("node", [HOOKS, event], {
      input,
      encoding: "utf8",
      env,
      cwd: options.cwd || root,
    });
  } catch (err) {
    return (err.stdout || "") + (err.stderr || "");
  }
}

function active(projectRoot = root) {
  const dir = path.join(projectRoot, "data", "control", "sessions");
  try {
    return fs
      .readdirSync(dir)
      .filter((name) => name.endsWith(".json"))
      .map((name) => JSON.parse(fs.readFileSync(path.join(dir, name), "utf8")));
  } catch (_) {
    return [];
  }
}

function events(file, projectRoot = root) {
  try {
    return fs
      .readFileSync(path.join(projectRoot, "logs", file), "utf8")
      .split("\n")
      .filter(Boolean)
      .map((line) => JSON.parse(line));
  } catch (_) {
    return [];
  }
}

// --- Overlapping sessions keep their own identity ---------------------------

const startA = run("session-start", { session_id: "A" });
check("session-start A exits cleanly", startA.includes("Hackathon Agent Kit session context"), startA.slice(0, 80));
run("session-start", { session_id: "B" });
check("both sessions are registered", active().length === 2, JSON.stringify(active()));

run("tool-use", { session_id: "A", tool_name: "alpha" });
const alpha = events("session-events.jsonl").find((e) => e.tool === "alpha");
check("explicit session id owns its event", alpha && alpha.session_id === "A", JSON.stringify(alpha));

run("tool-use", { session_id: "A", tool: { name: "nested-tool" } });
const nested = events("session-events.jsonl").find((e) => e.tool === "nested-tool");
check("nested tool object is normalized to its name", Boolean(nested), JSON.stringify(events("session-events.jsonl").slice(-1)));

run("tool-use", { tool_name: "beta" });
const beta = events("session-events.jsonl").find((e) => e.tool === "beta");
check("fallback attributes to the newest session", beta && beta.session_id === "B", JSON.stringify(beta));

run("stop", { session_id: "A" });
const remaining = active().map((entry) => entry.id);
check("stop removes only its own session", remaining.length === 1 && remaining[0] === "B", JSON.stringify(remaining));
const ended = events("sessions.jsonl").find((e) => e.type === "session_end" && e.session_id === "A");
check("session_end recorded for A", Boolean(ended), JSON.stringify(ended));

run("tool-use", { tool_name: "gamma" });
const gamma = events("session-events.jsonl").find((e) => e.tool === "gamma");
check("B keeps ownership after A stops", gamma && gamma.session_id === "B", JSON.stringify(gamma));

run("stop", { session_id: "B" });
check("registry empties after both stop", active().length === 0, JSON.stringify(active()));

// --- Distinct ids keep distinct files after sanitizing -----------------------

run("session-start", { session_id: "a/b" });
run("session-start", { session_id: "a-b" });
check("sanitized ids do not collide", active().length === 2, JSON.stringify(active()));
run("stop", { session_id: "a/b" });
const afterCollisionStop = active().map((entry) => entry.id);
check("stopping one colliding id keeps the other", afterCollisionStop.length === 1 && afterCollisionStop[0] === "a-b", JSON.stringify(afterCollisionStop));
run("stop", { session_id: "a-b" });
check("collision fixtures cleaned up", active().length === 0, JSON.stringify(active()));

// --- Stale sessions expire ---------------------------------------------------

const sessionDirForTtl = path.join(root, "data", "control", "sessions");
fs.mkdirSync(sessionDirForTtl, { recursive: true });
const stalePath = path.join(sessionDirForTtl, "stale.json");
fs.writeFileSync(stalePath, JSON.stringify({ id: "stale", started_at: "2020-01-01T00:00:00Z" }) + "\n");
const old = new Date("2020-01-01T00:00:00Z");
fs.utimesSync(stalePath, old, old);
run("tool-use", { tool_name: "after-stale" });
const afterStale = events("session-events.jsonl").find((e) => e.tool === "after-stale");
check("stale session is not used as fallback", afterStale && afterStale.session_id === "", JSON.stringify(afterStale));
check("stale session file is pruned", !fs.existsSync(stalePath), "stale.json still exists");

// --- Session ids cannot escape the state directory ---------------------------

run("session-start", { session_id: "../../escape" });
const sessionDir = path.join(root, "data", "control", "sessions");
const names = fs.readdirSync(sessionDir);
check("unsafe session id is sanitized on disk", names.length === 1 && !names[0].includes("..") && !names[0].includes("/"), JSON.stringify(names));
check("no file escaped the state directory", !fs.existsSync(path.join(root, "data", "control", "..", "..", "escape.json")), "escaped file exists");
run("stop", { session_id: "../../escape" });
check("sanitized id is removed by stop", active().length === 0, JSON.stringify(active()));

// --- Cached plugin root must not win over the project ------------------------

const project = fs.mkdtempSync(path.join(os.tmpdir(), "hackathonkit-project-"));
fs.mkdirSync(path.join(project, "configs"), { recursive: true });
fs.writeFileSync(path.join(project, "configs", "routing.yaml"), "routing:\n  mode: normal\n  version: 3\n");
markHackathonKitRepo(project);
const cache = fs.mkdtempSync(path.join(os.tmpdir(), "hackathonkit-cache-"));
const cachePlugin = path.join(cache, "plugins", "cache", "hackathonkit-local", "hackathonkit", "0.11.1");
fs.mkdirSync(cachePlugin, { recursive: true });

const cached = run("session-start", { session_id: "cache-1" }, {
  cwd: project,
  env: { HACKATHONKIT_REPO_ROOT: undefined, PLUGIN_ROOT: cachePlugin },
});
check("cached plugin uses the project as root", cached.includes("routing: normal"), cached.slice(0, 120));
check("project receives the session state", active(project).length === 1, JSON.stringify(active(project)));
check("cache receives no logs", !fs.existsSync(path.join(cache, "logs")), "cache/logs exists");
fs.rmSync(cache, { recursive: true, force: true });
fs.rmSync(project, { recursive: true, force: true });

// --- Windows .exe binary is probed -------------------------------------------

const exeProject = fs.mkdtempSync(path.join(os.tmpdir(), "hackathonkit-exe-"));
fs.mkdirSync(path.join(exeProject, "configs"), { recursive: true });
fs.writeFileSync(path.join(exeProject, "configs", "routing.yaml"), "routing:\n  mode: normal\n  version: 1\n");
markHackathonKitRepo(exeProject);
fs.mkdirSync(path.join(exeProject, "bin"), { recursive: true });
const exePath = path.join(exeProject, "bin", "hackathonkit-mcp.exe");
if (process.platform === "win32") {
  // A non-PE fixture cannot execute on Windows; use the real binary when it is
  // built (CI builds bin/hackathonkit-mcp.exe before this test).
  const realExe = path.join(process.cwd(), "bin", "hackathonkit-mcp.exe");
  if (fs.existsSync(realExe)) {
    fs.copyFileSync(realExe, exePath);
    const exeOut = run("session-start", { session_id: "exe-1" }, {
      cwd: exeProject,
      env: { HACKATHONKIT_REPO_ROOT: undefined, PLUGIN_ROOT: undefined },
    });
    check(".exe binary context is used", exeOut.includes("Hackathon Agent Kit session context"), exeOut.slice(0, 120));
  } else {
    console.log("[skip] .exe fixture requires a real PE binary; build bin/hackathonkit-mcp.exe first");
  }
} else {
  fs.writeFileSync(exePath, '#!/bin/sh\necho FAKE_BINARY_CONTEXT\n');
  fs.chmodSync(exePath, 0o755);
  const exeOut = run("session-start", { session_id: "exe-1" }, {
    cwd: exeProject,
    env: { HACKATHONKIT_REPO_ROOT: undefined, PLUGIN_ROOT: undefined },
  });
  check(".exe binary context is used", exeOut.includes("FAKE_BINARY_CONTEXT"), exeOut.slice(0, 120));
}
fs.rmSync(exeProject, { recursive: true, force: true });

fs.rmSync(root, { recursive: true, force: true });
if (failures > 0) {
  console.log("[hooks-test] " + failures + " assertion(s) failed");
  process.exit(1);
}
console.log("[hooks-test] all assertions passed");
