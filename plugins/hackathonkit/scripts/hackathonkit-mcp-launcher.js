// Platform-neutral MCP launcher for the Hackathon Agent Kit plugin.
//
// Claude Code (and other harnesses that only know one command) execute this
// file directly; Node is the only runtime those harnesses are guaranteed to
// have. It dispatches to the POSIX launcher on unix and the .cmd launcher on
// Windows, so a single plugin.json entry works on both platforms.
const { spawnSync } = require("node:child_process");
const path = require("node:path");
const fs = require("node:fs");

const dir = __dirname;
const isWindows = process.platform === "win32";
const launcher = path.join(dir, isWindows ? "hackathonkit-mcp-launcher.cmd" : "hackathonkit-mcp-launcher.sh");

if (!fs.existsSync(launcher)) {
  process.stderr.write(`Hackathon Agent Kit launcher: ${launcher} not found\n`);
  process.exit(1);
}

const result = isWindows
  ? spawnSync(launcher, process.argv.slice(2), { stdio: "inherit" })
  : spawnSync("sh", [launcher, ...process.argv.slice(2)], { stdio: "inherit" });

if (result.error) {
  process.stderr.write(`Hackathon Agent Kit launcher: ${result.error.message}\n`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
