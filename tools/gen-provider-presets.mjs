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
  // 注册表 models 是对象数组（{id, kind}）；排除 embedding/rerank/语音等非对话模态，
  // 免费信号（:free / -free 后缀）排前面，供 Go 侧 ModelIDs 种子直接呈现免费模型。
  const modelIDs = (() => {
    const out = [];
    const seen = new Set();
    for (const m of mod.models || []) {
      const id = typeof m === "string" ? m : (m && m.id) || "";
      if (!id || seen.has(id)) continue;
      if (m && /(embedding|rerank|tts|audio|image|speech|moderation)/i.test(String(m.kind || ""))) continue;
      if (/(embedding|rerank|tts|whisper|image|speech)/i.test(id)) continue;
      seen.add(id);
      out.push(id);
    }
    out.sort((a, b) => (/-?free$/i.test(b) ? 1 : 0) - (/-?free$/i.test(a) ? 1 : 0));
    return out;
  })();

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
    modelIDs: modelIDs.length ? modelIDs.slice(0, 40) : undefined,
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
  "http://127.0.0.1:8000/v1", "http://127.0.0.1:11434/v1",
]);
const outDir = process.argv[3] || path.join(__dirname, "..", "internal", "providerreg", "presets");
fs.mkdirSync(outDir, { recursive: true });
const used = new Set();
const notices = [];
let written = 0;
for (const p of presets) {
  if (used.has(p.key)) continue;
  used.add(p.key);
  if (SKIP_IDS[p.key]) { notices.push([p.key, SKIP_IDS[p.key] + ", skipped"]); continue; }
  const normBase = p.baseURL.replace(/\/\/localhost\b/, "//127.0.0.1");
  if (EXISTING_BASE.has(normBase)) { notices.push([p.key, "duplicate of existing hand-written preset, skipped"]); continue; }
  if (EXCLUDE_KEYS.some((re) => re.test(p.key))) { notices.push([p.key, "non chat/completions source (web/speech), skipped"]); continue; }
  const preset = {
    id: p.key,
    name: p.displayName,
    baseURL: p.baseURL,
    ...(p.headers ? { headers: p.headers } : {}),
    ...(p.notes ? { notes: p.notes } : {}),
    freeTier: p.hasFree,
    ...(p.modelIDs ? { modelIds: p.modelIDs.slice(0, 40) } : {}),
    ...(p.modelsFetcher ? { modelsFetcher: { url: p.modelsFetcher.url, type: p.modelsFetcher.type } } : {}),
    kind: p.kind,
  };
  fs.writeFileSync(path.join(outDir, `${p.key}.json`), JSON.stringify(preset, null, 2) + "\n");
  written++;
}
for (const [id, why] of skipped) notices.push([id, why]);
process.stderr.write(`wrote ${written} presets to ${outDir}\n`);
process.stderr.write(`skipped ${notices.length}:\n`);
for (const [id, why] of notices.slice(0, 80)) process.stderr.write(`  ${id}: ${why}\n`);
