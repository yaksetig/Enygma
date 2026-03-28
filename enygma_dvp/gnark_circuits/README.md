# gnark_circuits

REST API server (port **8081**) that generates Groth16 proofs using [gnark](https://github.com/ConsenSys/gnark).
Go module name: `gnark_server`.

---

## Directory structure

```
gnark_circuits/
├── main.go            ← API server entry point  (run this to start the server)
├── generation.go      ← One-time proving/verifying key generation  (do NOT run with main.go)
├── server/
│   ├── api/           ← HTTP route wiring
│   ├── circuits/      ← Per-circuit proof handlers (one sub-package per circuit)
│   └── config/        ← Port and key file paths (config.go)
├── templates/         ← gnark circuit definitions (Define() methods)
├── primitives/        ← Shared circuit gadgets (MerkleProof, Poseidon, nullifier, etc.)
├── poseidon/          ← Native Go Poseidon implementation used outside circuits
├── scripts/
│   └── keys/          ← Proving and verifying key files (*.key) — committed to repo
├── cmd/
│   └── export_vk_init/ ← Exports VKs to circom JSON format consumed by init.go
└── utils/             ← Misc helpers
```

---

## Starting the server

**Always use `go run main.go`, never `go run .`**

```bash
cd gnark_circuits
go run main.go
# Server starts on :8081
```

`go run .` will fail because both `main.go` and `generation.go` declare `package main`
and each has its own `main()` function. They are intentionally separate entry points
and cannot be compiled together.

The server must be started from the `gnark_circuits/` directory because key paths in
`server/config/config.go` are relative (e.g. `./scripts/keys/JoinErc20PK.key`).

---

## Proving/verifying keys

Keys are pre-generated and committed under `scripts/keys/`. You only need to regenerate
them if you change a circuit's `Define()` method in `templates/`.

To regenerate, run `generation.go` as a standalone program (takes several minutes):

```bash
cd gnark_circuits
go run generation.go
# Writes new *.key files to scripts/keys/
```

Don't run `generation.go` while the server is running — key files are overwritten in place.

After regenerating, re-export the verifying keys so `scripts/init.go` picks up the changes:

```bash
cd gnark_circuits
go run ./cmd/export_vk_init/ ../build
# Writes one JSON file per circuit to build/
```

Then re-run `init.go` to register the new VKs on-chain.

---

## Configuration

Port and key file paths are hardcoded in `server/config/config.go`.
To change the port, edit the `Port` field in `Load()`.

---

## Adding a new circuit

1. Add the circuit definition in `templates/` (implement `frontend.Circuit` and `Define()`).
2. Add a proof handler in `server/circuits/<circuitName>/`.
3. Wire the route in `server/api/server.go`.
4. Add the new PK/VK paths to `server/config/config.go`.
5. Add the circuit setup call in `generation.go` (`GenerationVkPk()`).
6. Regenerate keys and re-export VKs (see above).
