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

// On Windows a .cmd shim can only be started through cmd.exe, which re-parses
// the command line and interprets metacharacters even when libuv passes argv
// elements separately (the BatBadBut class). Refuse to forward any argument
// that could break out of cmd.exe quoting rather than relying on libuv.
function assertCmdSafeArguments(args) {
  for (const arg of args) {
    if (/[\r\n&|<>^%()!"]/.test(arg)) {
      process.stderr.write(
        `Hackathon Agent Kit launcher: refusing to forward argument with cmd.exe metacharacters: ${JSON.stringify(arg)}\n`
      );
      process.exit(1);
    }
  }
}
if (isWindows) {
  // Validate the launcher path too: cmd.exe re-parses the whole command line,
  // including the (install-derived) path, so a crafted install path with
  // metacharacters could break out of quoting.
  assertCmdSafeArguments([launcher, ...process.argv.slice(2)]);
}

const result = isWindows
  // .cmd/.bat files are not executable on their own under Node; launch through
  // the command interpreter. Pass the cmd file and args as separate argv
  // elements (not one pre-joined string) so libuv quotes spaces correctly.
  ? spawnSync(process.env.ComSpec || "cmd.exe", ["/d", "/s", "/c", launcher, ...process.argv.slice(2)], { stdio: "inherit" })
  : spawnSync("sh", [launcher, ...process.argv.slice(2)], { stdio: "inherit" });

if (result.error) {
  process.stderr.write(`Hackathon Agent Kit launcher: ${result.error.message}\n`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
