// Find Chinese string literals outside t() calls in admin JS (user-visible candidates).
const fs = require("fs");
const zh = /[\u4e00-\u9fff]/;
for (const f of process.argv.slice(2)) {
  const lines = fs.readFileSync(f, "utf8").split("\n");
  lines.forEach((line, i) => {
    if (!zh.test(line)) return;
    const trimmed = line.trim();
    if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*")) return;
    // strip comments at line end
    const code = line.replace(/\/\/[^'"]*$/, "");
    // crude: find quoted literals (', ", `) containing Chinese
    const lits = code.match(/'[^'\n]*'|"[^"\n]*"|`[^`\n]*`/g) || [];
    for (const lit of lits) {
      if (!zh.test(lit)) continue;
      // allow: literal already wrapped by t( ... ) on this line right before it
      const idx = code.indexOf(lit);
      const before = code.slice(Math.max(0, idx - 6), idx);
      if (/t\($/.test(before)) continue;
      // dictionary definition lines in 01_core.js: key: 'value' pattern with en value after
      if (f.endsWith("01_core.js")) continue;
      console.log(f + ":" + (i + 1) + ": " + lit.slice(0, 100));
    }
  });
}
