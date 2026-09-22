# Enygma Retail Payments — presentation demo

This folder is intentionally separate from `../demo`. It is a presentation-first UI simulator
for rehearsals and screenshots; it does **not** call Hardhat, gnark, the relayer, or any deployed
contract. The real-stack demo can continue evolving in `../demo` without sharing files or state.

## Run

Either open `index.html` directly, or serve the folder on loopback:

```bash
./run.sh
```

Then open <http://127.0.0.1:4173>.

The session persists in the browser tab through refreshes. **Restart rehearsal** clears it.

## Guided story

1. Register eight aligned registry rows; Alice, Bob, and Charlie each receive a 100-token note.
2. Switch among Alice, Bob, and Charlie. Only the active identity exposes Reveal/Copy controls.
3. Preview and establish a directional channel using None, Subset, Rift, or Full visibility.
4. Send one or more private payments. The smallest sufficient unspent note is selected.
5. Switch parties and scan: excluded rows skip, decoys fail AEAD, and recipients verify a note.

All hashes, blocks, gas values, proof stages, ML-KEM artifacts, receipts, and transaction IDs are
plausible presentation data generated locally in the browser. Do not use this build to claim that
the cryptographic or contract stack is running.
