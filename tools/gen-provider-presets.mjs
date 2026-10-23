#!/usr/bin/env node
// Mine the ExtremeRouter provider registry into cline2api presets (Go).
import fs from "fs";
import path from "path";
import { fileURLToPath, pathToFileURL } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const dir = process.argv[2] || "/home/dhan3713/extremerouter/open-sse/providers/registry";
const files = fs.readdirSync(dir).filter((f) => f.endsWith(".js") && f !== "index.js" && f !== "README.js");

const presets = [];
const skipped = [];

for (const f of files) {
  let mod;
  try {
    mod = (await import(pathToFileURL(path.join(dir, f)).href)).default;
  } catch (e) {
    skipped.push([path.basename(f, ".js"), `import failed: ${e.message}`]);
    continue;
  }
  if (!mod || typeof mod !== "object") continue;
  const category = mod.category;
  const transport = mod.transport || {};
  const format = transport.format || "openai";
  const baseUrl = transport.baseUrl || (Array.isArray(transport.baseUrls) ? transport.baseUrls[0] : "");
  const name = mod.display?.name || mod.id;
  const headers = {};
  for (const [k, v] of Object.entries(transport.headers || {})) {
    if (/^(accept|x-amz|user-agent|content-type)/i.test(k)) continue;
    headers[k] = String(v);
  }
  if (category !== "free" && category !== "freeTier" && !(category === "apikey" && (mod.hasFree || mod.freeModes))) {
    continue;
  }
  if (format !== "openai") {
    skipped.push([mod.id, `non-OpenAI transport ${format}; requires protocol adapter`]);
    continue;
  }
  const base = String(baseUrl).replace(/\/+(chat\/completions|responses)$/i, "");
  if (!base) { skipped.push([mod.id, "no chat/completions-capable baseURL"]); continue; }
  const fetcher = transport.modelsFetcher || mod.modelsFetcher || {};
  const modelIDs = (mod.models || []).filter((m) => typeof m === "string");

  const tiered = category === "free" || category === "freeTier";
  const host = (() => { try { return new URL(base).hostname; } catch { return ""; } })();
  const isLocal = host === "localhost" || host === "127.0.0.1" || host === "::1";
  // kind 决定 UI 分组优先级：free(免密钥) > freeTier(免费额度) > hasFree(含免费模型) > local(本地) > other。
  const kind = mod.noAuth === true ? "free" : tiered ? "freeTier" : isLocal ? "local" : "hasFree";
  presets.push({
    key: mod.id,
    displayName: name,
    baseURL: base,
    kind,
    hasFree: tiered || mod.noAuth === true,
    headers: Object.keys(headers).length ? headers : undefined,
    modelsFetcher: fetcher?.url ? { url: fetcher.url, type: fetcher.type || "openai" } : undefined,
    modelIDs: modelIDs.length ? modelIDs.slice(0, 12) : undefined,
    notes: `${mod.id} (from extremerouter registry)`
  });
}

const SKIP_IDS = {
  qwen2api: "personal netlify deployment of ExtremeRouter, not a public provider",
};
const EXCLUDE_KEYS = [/tts/i, /voice/i, /-web$/, /web-cookie/];
const EXISTING_BASE = new Set([
  "https://openrouter.ai/api/v1", "https://api.groq.com/openai/v1",
  "https://api.cerebras.ai/v1", "https://generativelanguage.googleapis.com/v1beta/openai",
  "https://api.mistral.ai/v1", "https://api.together.xyz/v1",
  "https://api.deepseek.com/v1", "https://api.openai.com/v1",
  "http://127.0.0.1:8000/v1",
]);
const gq = (s) => String(s ?? "").replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, " ");
let go = `// ============================================================================
// providerPresetsImported — generated from the ExtremeRouter provider registry
// (doc/cline2api-extremerouter-audit.md §7.3). DO NOT hand-edit: regenerate with
//   node tools/gen-provider-presets.mjs > providers_presets_generated.go
// Rules: category "free"/"freeTier" → FreeTier:true; hasFree apikey entries are
// imported with FreeTier:false (per-model tiering lands with the §9.3
// importer); non-OpenAI transports and existing presets are skipped below.
// ============================================================================

package main

var providerPresetsImported = map[string]providerPreset{
`;
const used = new Set();
const notices = [];
for (const p of presets) {
  if (used.has(p.key)) continue;
  used.add(p.key);
  if (SKIP_IDS[p.key]) { notices.push([p.key, SKIP_IDS[p.key] + ", skipped"]); continue; }
  const normBase = p.baseURL.replace(/\/\/localhost\b/, "//127.0.0.1");
  if (EXISTING_BASE.has(normBase)) { notices.push([p.key, "duplicate of existing preset baseURL, skipped"]); continue; }
  if (EXCLUDE_KEYS.some((re) => re.test(p.key))) { notices.push([p.key, "non chat/completions source (web/speech), skipped"]); continue; }
  const h = p.headers
    ? `\n\t\tHeaders: map[string]string{${Object.entries(p.headers).map(([k, v]) => `"${gq(k)}": "${gq(v)}"`).join(", ")}},`
    : "";
  const mf = p.modelsFetcher ? `\n\t\tModelsFetcher: &modelsFetcher{URL: "${gq(p.modelsFetcher.url)}", Type: "${gq(p.modelsFetcher.type)}"},` : "";
  const ids = p.modelIDs ? `\n\t\tModelIDs: []string{${p.modelIDs.slice(0, 8).map(gq).map((s) => `"${s}"`).join(", ")}},` : "";
  go += `\t"${gq(p.key)}": {\n\t\tName: "${gq(p.displayName)}",\n\t\tBaseURL: "${gq(p.baseURL)}",${h}${mf}${ids}\n\t\tKind: "${gq(p.kind)}",\n\t\tFreeTier: ${p.hasFree},\n\t\tNotes: "${gq(p.notes || "")}",\n\t},\n`;
}
for (const [id, why] of skipped) notices.push([id, why]);
go += `\n\t// Skipped during generation:\n`;
for (const [id, why] of notices.slice(0, 80)) go += `\t// ${gq(id)}: ${gq(why)}\n`;
go += `}\n`;
process.stdout.write(go);
