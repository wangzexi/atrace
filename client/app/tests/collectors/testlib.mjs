// Loads the conversion half of a collector for tests. A collector file runs as a
// script inside ego, so it cannot be imported; everything above the marker line
// touches no browser or network, and is evaluated here in an isolated context.
import { readFileSync } from "node:fs";
import vm from "node:vm";

export const MARKER = "// ==== side effects below";

// loadConversion returns the named top-level functions of the collector file.
// Results are cloned to plain data except for the functions named in uncloned. assign sets top-level variables of the
// file after it is loaded (the account the fixtures were synthetic for, which a collector learns when it runs).
export function loadConversion(file, names, { uncloned = [], assign = {}, globals = {} } = {}) {
  const source = readFileSync(file, "utf8").split("\n// ==== collector body ====\n").at(-1);
  const end = source.indexOf(MARKER);
  if (end < 0) throw new Error(`${file}: marker line "${MARKER}" not found`);
  const context = vm.createContext({ URL, URLSearchParams, TextEncoder, TextDecoder, crypto: globalThis.crypto, ...globals });
  const loaded = vm.runInContext(`${source.slice(0, end)}\n${Object.entries(assign).map(([name, value]) => `${name} = ${JSON.stringify(value)};`).join("\n")}\n;({ ${names.join(", ")} })`, context, { filename: file });
  // Results come from another realm; round-tripping through JSON gives plain
  // objects that assert.deepEqual compares by value.
  return Object.fromEntries(Object.entries(loaded).map(([name, fn]) => uncloned.includes(name) ? [name, fn] : [name, (...args) => {
    const clone = value => (value === undefined ? undefined : JSON.parse(JSON.stringify(value)));
    const result = fn(...args);
    return typeof result?.then === "function" ? result.then(clone) : clone(result);
  }]));
}

// contractProblems lists the ways documents break what the core and the page
// rely on, regardless of platform. Platform tests also compare whole documents.
export function contractProblems(entries, platform) {
  const problems = [];
  const reference = /^[a-z]+:[a-z]+:.+/;
  for (const { document: d } of entries) {
    const label = `${d.type} ${d.id || d.object || ""}`;
    if (d.id && !d.id.startsWith(`${platform}:`)) problems.push(`${label}: id lacks the ${platform}: prefix`);
    for (const field of ["actor", "object", "target", "attributedTo", "inReplyTo"]) {
      if (d[field] !== undefined && (typeof d[field] !== "string" || !reference.test(d[field]))) problems.push(`${label}: ${field} must be an ID string`);
    }
    if (["Create", "Like", "Add", "Announce"].includes(d.type) && !d.actor) problems.push(`${label}: missing actor`);
    if (["Create", "Like", "Add", "Announce"].includes(d.type) && !d.object) problems.push(`${label}: missing object`);
    if (d.type === "Add" && !d.target) problems.push(`${label}: Add has no target collection`);
    if (d.type === "Announce" && !d.id) problems.push(`${label}: Announce has no id of its own`);
    if (d.published !== undefined && Number.isNaN(Date.parse(d.published))) problems.push(`${label}: published is not a time`);
    for (const a of d.attachment || []) {
      if (!/^https:\/\//.test(a.url || "")) problems.push(`${label}: attachment url is not https`);
    }
  }
  return problems;
}
