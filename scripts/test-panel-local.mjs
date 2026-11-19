import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

// CI and local development exercise the same init/serve CLI on a fresh root.
const manager = process.env.GATEWAY_BINARY;
const core = process.env.SING_BOX_BINARY;
const runtime = process.env.MANAGED_CORE_BINARY;
assert(
  manager && core && runtime,
  "Set GATEWAY_BINARY, SING_BOX_BINARY and MANAGED_CORE_BINARY",
);
const interrupted = new AbortController();
const signal = AbortSignal.any([
  interrupted.signal,
  AbortSignal.timeout(300000),
]);
const stop = () => interrupted.abort(new Error("Panel acceptance interrupted"));
process.once("SIGINT", stop);
process.once("SIGTERM", stop);

function launch(binary, args, options = {}) {
  const child = spawn(binary, args, {
    signal,
    stdio: ["pipe", "inherit", "inherit"],
    ...options,
  });
  const done = new Promise((resolve) => {
    let error;
    child.once("error", (value) => {
      error = value;
    });
    child.once("close", (code, reason) => resolve({ code, reason, error }));
  });
  child.stdin.on("error", () => {}); // An early exit is reported through done.
  return { child, done };
}
async function check(done, operation) {
  const result = await done;
  signal.throwIfAborted();
  if (result.error) throw result.error;
  assert.equal(
    result.code,
    0,
    `${operation} exited with ${result.code ?? result.reason}`,
  );
}

await mkdir(".test", { recursive: true });
const root = await mkdtemp(resolve(".test/panel-api-"));
const args = [
  "panel",
  "--root",
  root,
  "--core",
  resolve(core),
  "--runtime",
  resolve(runtime),
  "--no-system",
];
const password = randomBytes(24).toString("hex");
if (process.env.GITHUB_ACTIONS) console.log(`::add-mask::${password}`);
let panel,
  log = "",
  successful = false;
try {
  const init = launch(resolve(manager), [
    ...args,
    "--address",
    "127.0.0.1",
    "init",
  ]);
  init.child.stdin.end(JSON.stringify({ username: "reviewadmin", password }));
  await check(init.done, "Panel initialization");

  // Saved settings require a real port. Only serve overrides it temporarily.
  panel = launch(
    resolve(manager),
    [...args, "--listen", "127.0.0.1:0", "serve"],
    {
      stdio: ["pipe", "pipe", "pipe"],
    },
  );
  panel.child.stdin.end();
  let url,
    exited = false;
  const capture = (chunk) => {
    log = (log + chunk.toString()).slice(-65536);
    url ??= log.match(/http:\/\/127\.0\.0\.1:[1-9][0-9]*/)?.[0];
  };
  panel.child.stdout.on("data", capture);
  panel.child.stderr.on("data", capture);
  void panel.done.then(() => {
    exited = true;
  });
  const deadline = Date.now() + 20000;
  let ready = false;
  while (!exited && Date.now() < deadline) {
    signal.throwIfAborted();
    if (url) {
      try {
        const response = await fetch(url + "/healthz", {
          signal: AbortSignal.any([signal, AbortSignal.timeout(1000)]),
        });
        ready = response.ok && (await response.json()).ready === true;
        if (ready) break;
        await response.body?.cancel();
      } catch {
        signal.throwIfAborted();
      }
    }
    await sleep(100, undefined, { signal });
  }
  assert(ready && !exited, "Panel did not become ready within 20 seconds");
  const test = launch(process.execPath, ["scripts/test-panel-api.mjs"], {
    env: {
      ...process.env,
      PANEL_TEST_URL: url,
      PANEL_TEST_USERNAME: "reviewadmin",
      PANEL_TEST_PASSWORD: password,
    },
  });
  test.child.stdin.end();
  await check(test.done, "Panel API acceptance");
  if (process.env.PANEL_BROWSER_TEST === "1") {
    const browser = launch(
      process.env.PYTHON_BINARY || "python",
      ["scripts/test-panel-browser.py"],
      {
        env: {
          ...process.env,
          PANEL_TEST_URL: url,
          PANEL_TEST_USERNAME: "reviewadmin",
          PANEL_TEST_PASSWORD: password,
        },
      },
    );
    browser.child.stdin.end();
    await check(browser.done, "Panel browser acceptance");
  }
  successful = true;
} catch (error) {
  console.error(log);
  console.error(`Acceptance data retained at ${root}`);
  throw error;
} finally {
  if (panel) {
    panel.child.kill();
    const shutdown = new AbortController();
    const stopped = await Promise.race([
      panel.done.then(() => true),
      sleep(5000, false, { signal: shutdown.signal }).catch(() => true),
    ]);
    shutdown.abort();
    if (!stopped) {
      panel.child.kill("SIGKILL");
      await panel.done;
    }
  }
  process.removeListener("SIGINT", stop);
  process.removeListener("SIGTERM", stop);
  if (successful) await rm(root, { recursive: true, force: true });
}
