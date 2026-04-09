const { poseidon_gencontract } = require('circomlibjs');
const fs = require('fs');

const bc3 = poseidon_gencontract.createCode(2);
const bc5 = poseidon_gencontract.createCode(4);
const runtime3 = '0x' + bc3.slice(26);
const runtime5 = '0x' + bc5.slice(26);

const src = 'contracts/core/contracts/Poseidon.sol';

fs.writeFileSync(
  'artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT3.json',
  JSON.stringify({
    _format: 'hh-sol-artifact-1',
    contractName: 'PoseidonT3',
    sourceName: src,
    abi: poseidon_gencontract.generateABI(2),
    bytecode: bc3,
    deployedBytecode: runtime3,
    linkReferences: {},
    deployedBytecodeImmutables: {},
  }, null, 2)
);

fs.writeFileSync(
  'artifacts/contracts/core/contracts/Poseidon.sol/PoseidonT5.json',
  JSON.stringify({
    _format: 'hh-sol-artifact-1',
    contractName: 'PoseidonT5',
    sourceName: src,
    abi: poseidon_gencontract.generateABI(4),
    bytecode: bc5,
    deployedBytecode: runtime5,
    linkReferences: {},
    deployedBytecodeImmutables: {},
  }, null, 2)
);

console.log('Poseidon artifacts regenerated');
