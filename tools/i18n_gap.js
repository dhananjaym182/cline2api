const fs = require("fs");
const core = fs.readFileSync("adminui/js/01_core.js", "utf8");
const m = core.match(/const I18N\s*=\s*\{([\s\S]*?)\n\};/);
if (!m) { console.log("NO I18N BLOCK"); process.exit(1); }
const keys = new Set();
const kre = /^\s*'((?:[^'\\]|\\.)*)'\s*:/gm;
let k;
while ((k = kre.exec(m[1]))) keys.add(k[1]);
const body = fs.readFileSync("adminui/body.html", "utf8");
const zh = /[\u4e00-\u9fff]/;
const gaps = [];
const seen = new Set();
function decode(s) {
  return s.replace(/&(gt|lt|amp|quot|nbsp|#x?[0-9a-fA-F]+);/g, (_, e) => {
    if (e === "gt") return ">"; if (e === "lt") return "<"; if (e === "amp") return "&";
    if (e === "quot") return '"'; if (e === "nbsp") return " ";
    if (e[0] === "#") return String.fromCodePoint(e[1] === "x" ? parseInt(e.slice(2), 16) : parseInt(e.slice(1), 10));
    return _;
  });
}
function check(t, ctx) {
  t = decode(t.trim());
  if (!t || !zh.test(t)) return;
  if (keys.has(t)) return;
  const id = ctx + " | " + t.slice(0, 160);
  if (seen.has(id)) return;
  seen.add(id);
  gaps.push(id);
}
for (const p of body.split(/<[^>]*>/)) check(p, "text");
const attrRe = /(title|placeholder|aria-label)="([^"]*)"/g;
let a;
while ((a = attrRe.exec(body))) check(a[2], a[1]);
console.log("GAPS:", gaps.length);
for (const g of gaps) console.log(g);
