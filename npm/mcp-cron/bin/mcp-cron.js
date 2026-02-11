#!/usr/bin/env node

// SPDX-License-Identifier: AGPL-3.0-only

const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");

const PLATFORM_PACKAGES = {
  "darwin-arm64": "mcp-cron-darwin-arm64",
  "darwin-x64": "mcp-cron-darwin-amd64",
  "linux-x64": "mcp-cron-linux-amd64",
  "linux-arm64": "mcp-cron-linux-arm64",
  "win32-x64": "mcp-cron-windows-amd64",
  "win32-arm64": "mcp-cron-windows-arm64",
};

function getBinaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORM_PACKAGES[key];
  if (!pkg) {
    console.error(`Unsupported platform: ${key}`);
    console.error(`Supported platforms: ${Object.keys(PLATFORM_PACKAGES).join(", ")}`);
    process.exit(1);
  }

  const exe = process.platform === "win32" ? "mcp-cron.exe" : "mcp-cron";

  // Try require.resolve first (works when installed via npm)
  try {
    return path.join(
      path.dirname(require.resolve(`${pkg}/package.json`)),
      "bin",
      exe
    );
  } catch {
    // Fallback: look for binary in sibling directory (local development)
    const localPath = path.join(__dirname, "..", "..", pkg, "bin", exe);
    if (fs.existsSync(localPath)) {
      return localPath;
    }

    console.error(`Could not find the binary package "${pkg}" for your platform (${key}).`);
    console.error("This usually means the optional dependency was not installed.");
    console.error("Try reinstalling: npm install mcp-cron");
    process.exit(1);
  }
}

const binPath = getBinaryPath();
const child = spawn(binPath, process.argv.slice(2), {
  stdio: "inherit",
});

// Forward signals to the child process
for (const signal of ["SIGTERM", "SIGINT", "SIGHUP"]) {
  process.on(signal, () => {
    if (!child.killed) {
      child.kill(signal);
    }
  });
}

child.on("error", (err) => {
  console.error(`Failed to start mcp-cron: ${err.message}`);
  process.exit(1);
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.exit(1);
  }
  process.exit(code ?? 0);
});
