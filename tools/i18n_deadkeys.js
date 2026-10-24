// List I18N dict keys whose zh text appears nowhere outside the dict block itself.
// Usage: node tools/i18n_deadkeys.js
const fs = require('fs');
const dir = __dirname + '/../adminui';
const body = fs.readFileSync(dir + '/body.html', 'utf8');
const jsFiles = fs.readdirSync(dir + '/js').map(f => fs.readFileSync(dir + '/js/' + f, 'utf8'));
const core = jsFiles[jsFiles.findIndex(s => s.includes('const I18N'))];
const m = core.match(/const I18N\s*=\s*\{([\s\S]*?)\n\};/);
if (!m) { console.error('dict block not found'); process.exit(1); }
const keyRe = /^\s*'((?:[^'\\]|\\')+)':\s*'/gm;
const keys = [];
let k;
while ((k = keyRe.exec(m[1]))) keys.push({ key: k[1].replace(/\\'/g, "'"), idx: k.index + m.index });
const others = [body, ...jsFiles.filter(s => s !== core)];
// keys can also be used inside 01_core.js outside the dict block itself
const coreRest = core.replace(m[1], '');
others.push(coreRest);
const ctxOf = (key) => {
  for (const src of others) {
    if (src.includes("'" + key + "'") || src.includes('"' + key + '"') || src.includes(key)) return false;
  }
  return true;
};
let dead = [];
for (const { key } of keys) if (ctxOf(key)) dead.push(key);
console.log('DEAD KEYS: ' + dead.length);
dead.forEach(d => console.log('  ' + JSON.stringify(d)));
// also: duplicate identical keys
const all = m[1].match(/^\s*'((?:[^'\\]|\\')+)':/gm) || [];
const seen = new Map();
for (const a of all) { seen.set(a.trim(), (seen.get(a.trim()) || 0) + 1); }
const dups = [...seen].filter(([, n]) => n > 1);
console.log('DUP KEYS: ' + dups.length);
dups.forEach(([kk, n]) => console.log('  ' + kk + ' x' + n));
