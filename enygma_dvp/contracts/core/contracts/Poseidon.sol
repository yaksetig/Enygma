// Copyright 2024-2025, Parity Holding Ltd.
// SPDX-License-Identifier: BUSL-1.1

pragma solidity ^0.8.0;
pragma abicoder v2;

// ============================================================
// WARNING — INTENTIONAL STUB IMPLEMENTATIONS
// ============================================================
//
// PoseidonT3 and PoseidonT5 below have EMPTY function bodies.
// They exist only so that Solidity can resolve the library
// symbols during compilation. The real EVM bytecode is injected
// at deploy time from the circomlibjs-generated artifacts:
//
//   artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT3.json
//   artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT5.json
//
// CRITICAL: Running `npx hardhat compile` will OVERWRITE those
// artifact files with the stub bytecode below, causing every
// on-chain Poseidon call to return 0. This silently breaks all
// Merkle root checks and ZK proof verification.
//
// After any `npx hardhat compile`, regenerate the real artifacts
// by running this command from the repo root:
//
//   node -e "
//   const { poseidon_gencontract } = require('circomlibjs');
//   const fs = require('fs');
//   const bc3 = poseidon_gencontract.createCode(2);
//   const bc5 = poseidon_gencontract.createCode(4);
//   const runtime3 = '0x' + bc3.slice(2+24);
//   const runtime5 = '0x' + bc5.slice(2+24);
//   const a3 = {_format:'hh-sol-artifact-1',contractName:'PoseidonT3',sourceName:'contracts/core/contracts/Poseidon.sol',abi:poseidon_gencontract.generateABI(2),bytecode:bc3,deployedBytecode:runtime3,linkReferences:{},deployedBytecodeImmutables:{}};
//   const a5 = {_format:'hh-sol-artifact-1',contractName:'PoseidonT5',sourceName:'contracts/core/contracts/Poseidon.sol',abi:poseidon_gencontract.generateABI(4),bytecode:bc5,deployedBytecode:runtime5,linkReferences:{},deployedBytecodeImmutables:{}};
//   fs.writeFileSync('artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT3.json', JSON.stringify(a3,null,2));
//   fs.writeFileSync('artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT5.json', JSON.stringify(a5,null,2));
//   console.log('done');
//   "
//
// Correct artifact sizes (verify after regeneration):
//   PoseidonT3.json  ~9755 bytes deployedBytecode
//   PoseidonT5.json  ~16276 bytes deployedBytecode
// ============================================================

library PoseidonT3 {
    // solhint-disable-next-line no-empty-blocks
    function poseidon(uint256[2] memory input) public pure returns (uint256) {}
}

library PoseidonT5 {
    // solhint-disable-next-line no-empty-blocks
    function poseidon(uint256[4] memory input) public pure returns (uint256) {}
}