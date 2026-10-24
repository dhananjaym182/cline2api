const fs = require("fs");
const core = fs.readFileSync("adminui/js/01_core.js", "utf8");
const m = core.match(/const I18N\s*=\s*\{([\s\S]*?)\n\};/);
if (!m) { console.log("NO I18N BLOCK"); process.exit(1); }
const kre = /^\s*'((?:[^'\\]|\\.)*)'\s*:\s*'((?:[^'\\]|\\.)*)'/gm;
let k, seen = {}, dups = [], apostropheValues = [];
const valSeen = {}; const valCollisions = [];
while ((k = kre.exec(m[1]))) {
  if (seen[k[1]]) dups.push(k[1]);
  seen[k[1]] = 1;
  if (k[2].includes("'")) apostropheValues.push(k[1] + " => " + k[2]);
  if (valSeen[k[2]] && valSeen[k[2]] !== k[1]) valCollisions.push(k[2] + " <- [" + valSeen[k[2]] + "] and [" + k[1] + "]");
  if (!valSeen[k[2]]) valSeen[k[2]] = k[1];
}
console.log("keys:", Object.keys(seen).length);
console.log("dup keys:", dups.length, dups.join(" | "));
console.log("apostrophe EN values:", apostropheValues.length, apostropheValues.slice(0, 5).join(" | "));
console.log("EN value collisions:", valCollisions.length);
for (const c of valCollisions.slice(0, 20)) console.log("  " + c);
