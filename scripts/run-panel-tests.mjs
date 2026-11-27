import { spawn } from "node:child_process";
import { readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";

// Unit scripts must release their own DOM/timers. The deadline reports a leaked
// process as failure and stops the suite instead of stalling the whole CI job.
const directory = fileURLToPath(new URL("./", import.meta.url));
const excluded = new Set(["test-panel-api.mjs", "test-panel-local.mjs"]);
const scripts = (await readdir(directory)).filter(
  (name) => /^test-panel(?:-[\w-]+)?\.mjs$/.test(name) && !excluded.has(name),
);
const selected = process.argv.slice(2);
for (const name of selected) {
  if (!scripts.includes(name))
    throw new Error(`Unknown panel unit script: ${name}`);
}
for (const name of selected.length ? selected : scripts.sort()) {
  console.log(`Running ${name}`);
  const code = await new Promise((resolve, reject) => {
    const child = spawn(
      process.execPath,
      [fileURLToPath(new URL(name, import.meta.url))],
      {
        stdio: "inherit",
        windowsHide: true,
      },
    );
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      console.error(
        `${name} exceeded 60 seconds; check pending requests and lifecycle cleanup.`,
      );
      child.kill();
    }, 60000);
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
    child.once("exit", (code) => {
      clearTimeout(timer);
      resolve(timedOut ? 1 : (code ?? 1));
    });
  });
  if (code !== 0) {
    process.exitCode = code;
    break;
  }
}
