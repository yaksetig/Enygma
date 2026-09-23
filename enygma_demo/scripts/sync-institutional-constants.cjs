// Rebuild browser constants from the protocol's own Poseidon tables.
const fs = require("fs");
const path = require("path");
const source = fs.readFileSync(path.join(__dirname, "../../enygma_payments/gnark-server/poseidon/constants.go"), "utf8");
const constants = {};
for (const kind of ["C", "S", "M", "P"]) {
  const section = source.split(`func GetPoseidon${kind}(`)[1].split(/\nfunc /)[0];
  constants[kind] = [2, 3, 4].map(t => {
    if (kind === "C" || kind === "S") {
      const block = section.match(new RegExp(`constantHex${t}\\s*:=\\s*\\[\\]string\\s*\\{([\\s\\S]*?)\\}`))[1];
      return [...block.matchAll(/"(0x[\da-f]+)"/g)].map(match => match[1]);
    }
    const block = section.split(`case ${t}:`)[1].split(/case |default:/)[0];
    const values = [...block.matchAll(/SetString\("(0x[\da-f]+)"/g)].map(match => match[1]);
    if (values.length !== t * t) throw new Error(`Invalid ${kind} matrix for t=${t}`);
    return Array.from({ length: t }, (_, i) => values.slice(i * t, (i + 1) * t));
  });
}
const output = `// Generated from enygma_payments/gnark-server/poseidon/constants.go.\n// Regenerate with: node scripts/sync-institutional-constants.cjs\nexport const POSEIDON_CONSTANTS = ${JSON.stringify(constants)};\n`;
const target = path.join(__dirname, "../js/institutional-constants.js");
if (process.argv.includes("--check")) {
  if (fs.readFileSync(target, "utf8") !== output) throw new Error("Institutional Poseidon constants differ from protocol source");
} else fs.writeFileSync(target, output);
