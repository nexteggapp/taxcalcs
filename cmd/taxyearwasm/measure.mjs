// Times the taxyear WebAssembly build under node, as a browser would run it.
//
//   bazel build //cmd/taxyearwasm:taxyearwasm_wasm
//   node cmd/taxyearwasm/measure.mjs [CASE.jsonc]
//
// It reports the binary's size, then, with and without the rules engine's
// line-by-line working, each in a fresh instance: instantiating it, loading
// the engine, the first compute of Your plan alone, a one-field edit to it,
// the first compute of five scenarios, a one-field edit to a leaf scenario
// and to Your plan, and the linear memory at the end. An edit's time is
// applyEdit and the compute after it, which is what the page waits for;
// each is run five times.

import fs from "node:fs";
import path from "node:path";
import zlib from "node:zlib";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const casePath = process.argv[2] ?? path.join(root, "taxyear/testdata/cases/full-everything.jsonc");

// The newest build of the binary, whatever its configuration's directory.
function wasmPath() {
  const out = path.join(root, "bazel-out");
  const found = fs.readdirSync(out)
    .map((d) => path.join(out, d, "bin/cmd/taxyearwasm/taxyearwasm_wasm_/taxyearwasm_wasm"))
    .filter((p) => fs.existsSync(p))
    .sort((a, b) => fs.statSync(b).mtimeMs - fs.statSync(a).mtimeMs);
  if (found.length === 0) throw new Error("build //cmd/taxyearwasm:taxyearwasm_wasm first");
  return found[0];
}

// wasm_exec.js from the Go SDK Bazel built with.
const outputBase = path.resolve(fs.realpathSync(path.join(root, "bazel-out")), "../../..");
const wasmExec = path.join(outputBase, "external/rules_go++go_sdk+go_default_sdk/lib/wasm/wasm_exec.js");
await import(wasmExec);

// Protobuf's wire format, for the few Edit fields this script sends.
function varint(n) {
  const out = [];
  let v = BigInt(n);
  if (v < 0n) v += 1n << 64n;
  do {
    let b = Number(v & 0x7fn);
    v >>= 7n;
    if (v > 0n) b |= 0x80;
    out.push(b);
  } while (v > 0n);
  return out;
}
const field = (n, bytes) => [...varint((n << 3) | 2), ...varint(bytes.length), ...bytes];
const text = (n, s) => field(n, [...Buffer.from(s, "utf8")]);
const int = (n, v) => [...varint(n << 3), ...varint(v)];
const money = (minor) => [...int(1, minor), ...text(2, "USD")];
const date = (y, m, d) => [...int(1, y), ...int(2, m), ...int(3, d)];
// Edits{edits: [Edit{scenario, path, value}]}. Value field 1 is money and 3
// is a date.
function edit(scenario, p, valueField, value) {
  const e = [...text(1, scenario), ...text(2, p), ...field(3, field(valueField, value))];
  return Uint8Array.from(field(1, e));
}

// The case with only Your plan, and with a fifth scenario: a copy of its
// second under another id, so it is computed on its own.
function variants(jsonc) {
  const json = JSON.parse(jsonc.split("\n").filter((l) => !l.trim().startsWith("//")).join("\n"));
  const one = { ...json, scenarios: json.scenarios.slice(0, 1) };
  const five = { ...json, scenarios: [...json.scenarios] };
  while (five.scenarios.length < 5) {
    const s = json.scenarios[1];
    five.scenarios.push({ ...s, id: `${s.id}-copy-${five.scenarios.length}`, name: `${s.name} (copy)` });
  }
  return { one: JSON.stringify(one), five: JSON.stringify(five) };
}

function ok(r) {
  if (r.error) throw new Error(r.error);
  return r.bytes;
}

function time(f) {
  const t = performance.now();
  const out = f();
  return [out, performance.now() - t];
}

const mib = (n) => (n / (1 << 20)).toFixed(1);

async function run(binary, working, files) {
  const go = new globalThis.Go();
  const t0 = performance.now();
  const { instance } = await WebAssembly.instantiate(binary, go.importObject);
  go.run(instance);
  const instantiate = performance.now() - t0;
  const api = globalThis.taxyear;
  ok(api.setWorking(working));
  const [, load] = time(() => ok(api.load()));
  const one = ok(api.parse(files.one));
  const five = ok(api.parse(files.five));

  const rows = [];
  const step = (name, f) => {
    const [out, ms] = time(f);
    rows.push([name, `${ms.toFixed(0)} ms`, out.length]);
    return out;
  };
  // An edit is timed five times, each with a new value so nothing is
  // cached, and reported as the median and the slowest.
  const repeat = (name, f) => {
    const ms = [];
    let out;
    for (let i = 0; i < 5; i++) {
      const t = performance.now();
      out = f(i);
      ms.push(performance.now() - t);
    }
    ms.sort((a, b) => a - b);
    rows.push([name, `${ms[2].toFixed(0)} ms, slowest ${ms[4].toFixed(0)}`, out.length]);
  };
  const planEdit = (i) => edit("plan", "model.accounts[schwab].parts[schwab-1bd4miq1].year_end_distribution.amounts.capital_gain_distributions", 1, money(30000 + i));
  const leafEdit = (i) => edit("raise-freelance-stops", "model.businesses[reyes-design].parts[reyes-design-1pjpps90].stops.on", 3, date(2026, 10, 20 + i));
  step("first compute, Your plan alone", () => ok(api.compute(one, "plan")));
  repeat("edit Your plan alone, then compute", (i) => ok(api.compute(ok(api.applyEdit(one, planEdit(i))), "plan")));
  step("first compute, five scenarios", () => ok(api.compute(five, "raise-freelance-stops")));
  repeat("edit a leaf scenario of five, then compute", (i) => ok(api.compute(ok(api.applyEdit(five, leafEdit(i))), "raise-freelance-stops")));
  repeat("edit Your plan of five, then compute", (i) => ok(api.compute(ok(api.applyEdit(five, planEdit(10 + i))), "plan")));
  repeat("compute again, nothing changed", () => ok(api.compute(five, "raise-freelance-stops")));
  const memory = instance.exports.mem.buffer.byteLength;
  return { instantiate, load, rows, memory };
}

const binary = fs.readFileSync(wasmPath());
console.log(`binary: ${mib(binary.length)} MiB raw, ${mib(zlib.gzipSync(binary, { level: 9 }).length)} MiB gzipped`);
console.log(`case: ${path.relative(root, casePath)}`);
const files = variants(fs.readFileSync(casePath, "utf8"));
for (const working of [true, false]) {
  const r = await run(binary, working, files);
  console.log(`\nworking ${working ? "on" : "off"}`);
  console.log(`  ${"instantiate".padEnd(42)} ${r.instantiate.toFixed(0)} ms`);
  console.log(`  ${"load the rules engine".padEnd(42)} ${r.load.toFixed(0)} ms`);
  for (const [name, ms, size] of r.rows) {
    console.log(`  ${name.padEnd(42)} ${ms.padEnd(22)} page ${(size / 1024).toFixed(0)} KiB`);
  }
  console.log(`  ${"linear memory at the end".padEnd(42)} ${mib(r.memory)} MiB of 192`);
}
process.exit(0);
