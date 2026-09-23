#!/usr/bin/env node
const { spawn, spawnSync } = require("child_process");
const path = require("path");
const http = require("http");

const root = path.join(__dirname, "..");
const suites = ["deployment-test.cjs", "institutional-test.cjs", "demo-test.cjs", "source-test.cjs"];
const server = spawn(process.execPath, [path.join(__dirname, "static-server.cjs")], { cwd: root, stdio: ["ignore", "pipe", "pipe"] });

function ready() {
  return new Promise((resolve, reject) => {
    let attempts = 0;
    const probe = () => {
      const request = http.get("http://127.0.0.1:4193/", response => { response.resume(); resolve(); });
      request.on("error", () => attempts++ < 40 ? setTimeout(probe, 100) : reject(new Error("Test server did not start.")));
    };
    probe();
  });
}

(async () => {
  try {
    await ready();
    for (const suite of suites) {
      process.stdout.write(`\n▸ ${suite}\n`);
      const result = spawnSync(process.execPath, [path.join(__dirname, suite)], { cwd: root, encoding: "utf8", env: { ...process.env, DEMO_URL: "http://127.0.0.1:4193" } });
      process.stdout.write((result.stdout || "") + (result.stderr || ""));
      if (result.status !== 0) process.exitCode = 1;
    }
  } finally {
    server.kill();
  }
})().catch(error => { console.error(error); server.kill(); process.exit(1); });
