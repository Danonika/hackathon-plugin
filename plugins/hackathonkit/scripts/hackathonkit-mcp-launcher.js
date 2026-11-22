// Start the native MCP binary or Compose without reinterpreting user arguments.
"use strict";

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const args = process.argv.slice(2);

function run(command, argv, cwd) {
  const result = spawnSync(command, argv, { cwd, stdio: "inherit", shell: false });
  if (result.error) throw result.error;
  if (result.signal) return 128 + (os.constants.signals[result.signal] || 0);
  return result.status ?? 1;
}

function windowsLaunch() {
  const root = path.resolve(process.env.HACKATHONKIT_REPO_ROOT || path.join(__dirname, "../../.."));
  const moduleFile = fs.readFileSync(path.join(root, "go.mod"), "utf8");
  if (!/^module\s+hackathonkit\.local\/agentkit\s*$/m.test(moduleFile)) {
    throw new Error("repository root does not identify the Hackathon Agent Kit module");
  }
  if (args.some(arg => /^--?repo-root(?:=|$)/.test(arg))) {
    throw new Error("--repo-root is set by the launcher");
  }
  const bin = path.join(root, "bin");
  const executable = path.join(bin, "hackathonkit-mcp.exe");
  const dir = fs.lstatSync(bin, { throwIfNoEntry: false });
  const file = fs.lstatSync(executable, { throwIfNoEntry: false });
  if (dir?.isSymbolicLink() || file?.isSymbolicLink()) {
    throw new Error("refusing a linked local binary or bin directory");
  }
  if (file?.isFile()) return run(executable, ["--repo-root", root, ...args]);
  const compose = path.join(root, "docker-compose.yml");
  return run("docker.exe", ["compose", "-f", compose, "run", "--rm", "-T", "hackathonkit-mcp", ...args], root);
}

try {
  process.exitCode = process.platform === "win32"
    ? windowsLaunch()
    : run("/bin/sh", [path.join(__dirname, "hackathonkit-mcp-launcher.sh"), ...args]);
} catch (err) {
  process.stderr.write(`Hackathon Agent Kit launcher: ${err.message}\n`);
  process.exitCode = 1;
}
