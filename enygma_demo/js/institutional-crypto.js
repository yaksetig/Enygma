import { POSEIDON_CONSTANTS } from "./institutional-constants.js";

// Exact parameters and generators from contracts/enygma/contracts/CurveBabyJubJub.sol.
export const FIELD = 21888242871839275222246405745257275088548364400416034343698204186575808495617n;
export const ORDER = 2736030358979909402780800718157159386076813972158567259200215660948447373041n;
export const G = [12337812418750581066638756637363471856433191340622504180842886595232027947307n, 15225366398330386329633463986700597127113326976080712967801565482915963669722n];
export const H = [10100005861917718053548237064487763771145251762383025193119768015180892676690n, 7512830269827713629724023825249861327768672768516116945507944076335453576011n];
export const mod = (n, p = FIELD) => ((BigInt(n) % p) + p) % p;
export const scalar = n => mod(n, ORDER);
export const encodePoint = p => p.map(String);

function inverse(n) {
  let a = mod(n), b = FIELD, x = 1n, y = 0n;
  while (b) { const q = a / b; [a, b] = [b, a - q * b]; [x, y] = [y, x - q * y]; }
  if (a !== 1n) throw new Error("Invalid curve denominator");
  return mod(x);
}

export function pointAdd(p, q) {
  const [x, y] = p.map(BigInt), [u, v] = q.map(BigInt);
  const d = mod(168696n * x * u * y * v);
  return [mod((x * v + y * u) * inverse(1n + d)), mod((y * v - 168700n * x * u) * inverse(1n - d))];
}

export function pointMultiply(point, amount) {
  let n = scalar(amount), out = [0n, 1n], base = point;
  while (n) { if (n & 1n) out = pointAdd(out, base); base = pointAdd(base, base); n >>= 1n; }
  return out;
}

export const pedersen = (value, random) => pointAdd(pointMultiply(G, value), pointMultiply(H, random));
export const samePoint = (a, b) => a.every((value, i) => BigInt(value) === BigInt(b[i]));

const tables = Object.fromEntries(Object.entries(POSEIDON_CONSTANTS).map(([key, value]) => [key, value.map(row => row.map(v => Array.isArray(v) ? v.map(BigInt) : BigInt(v)))]));
const pow5 = n => { const sq = mod(n * n); return mod(sq * sq * n); };

// Same optimized rounds and matrix orientation as gnark-server/poseidon/poseidon.go.
export function poseidon(inputs) {
  if (inputs.length < 1 || inputs.length > 3) throw new Error("Poseidon accepts one to three inputs here");
  const t = inputs.length + 1, index = t - 2, rounds = [56, 57, 56][index];
  const C = tables.C[index], S = tables.S[index], M = tables.M[index], P = tables.P[index];
  const mix = (state, matrix) => state.map((_, i) => mod(state.reduce((sum, v, j) => sum + matrix[j][i] * v, 0n)));
  const ark = (state, offset) => state.map((v, i) => mod(v + C[offset + i]));
  let state = ark([0n, ...inputs.map(v => mod(v))], 0);
  for (let r = 0; r < 3; r++) state = mix(ark(state.map(pow5), (r + 1) * t), M);
  state = mix(ark(state.map(pow5), 4 * t), P);
  for (let r = 0; r < rounds; r++) {
    state[0] = mod(pow5(state[0]) + C[5 * t + r]);
    const first = state[0], offset = (2 * t - 1) * r;
    state = [mod(state.reduce((sum, v, i) => sum + S[offset + i] * v, 0n)), ...state.slice(1).map((v, i) => mod(v + first * S[offset + t + i]))];
  }
  for (let r = 0; r < 3; r++) state = mix(ark(state.map(pow5), 5 * t + rounds + r * t), M);
  return mix(state.map(pow5), M)[0];
}

export const spendScalar = value => scalar(BigInt(`0x${value.replace(/^spend_sk_/, "")}`));
export const spendPublic = secret => scalar(poseidon([spendScalar(secret), spendScalar(secret)]));
