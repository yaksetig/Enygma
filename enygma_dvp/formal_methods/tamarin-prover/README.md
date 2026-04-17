# Overview
This theory models a minimal shielded payment protocol in which Alice spends one of her private notes and pays Bob. 

Conceptually:

    1. Alice holds a note commitment commit_a together with the
       secrets (amount_a, salt_a) that open it, plus a
       serial number serial_alice assigned by the chain when the
       note was inserted into the commitment tree.

    2. To pay Bob, Alice:
         * derives a shared secret with Bob via asymmetric encryption,
         * constructs a fresh commitment commit_b for the amount
           she is sending,
         * publishes a nullifier (derived from her spend key and the
           serial number of the note she is spending) that
           invalidates her own note,
         * attaches a zero-knowledge proof linking all of the above,
         * wraps the amount and token info in an AEAD memo so that
           only Bob can read it.

    3. The chain accepts the transaction if the proof verifies and
       the nullifier has not been seen before. A fresh serial
       number serial_bob is drawn (modeling the tree index
       assignment that happens when Bob's commitment is inserted),
       and a new note record is added to the ledger.

    4. Bob scans the chain, decrypts the memo with his view key,
       and learns which note is now his. In this model Bob does
       not re-spend; Bob's acceptance is terminal.

Serial numbers vs salts: these are deliberately separated in this model. salt_a is the blinding randomness inside commit_a (for commitment hiding); serial_alice is a chain-assigned tree index used in nullifier derivation. This matches modern shielded protocols (e.g. Zcash Orchard) where the nullifier-derivation input is distinct from the commitment blinding.

  Public parameters: token_id and info are public system parameters,
  output by the Setup rule. The adversary knows both. This matches
  real deployments where asset identifiers and KDF domain-separation
  tags are part of the protocol specification, not secrets.

  This minimal model deliberately omits:
    * explicit anchor-root / Merkle membership proofs (serials
      abstract over the tree structure),
    * change notes (Alice keeping a residual output),
    * balance conservation (deferred to a separate Lean artifact),
    * liveness / reclaim (see scope note below),
    * Bob re-spending (terminal acceptance).

## Threat model:
    * Standard Dolev-Yao network attacker, also given token_id and
      info as public parameters.
    * Optionally, the adversary may compromise Bob's viewing key
      view_sk_B via the RevealBobViewKey rule. This models the
      scenario where Bob's auditor or a backup is leaked. The
      spend key spend_sk_B is not compromisable in this model
      because Bob does not spend; its compromise would not enable
      any additional attack in scope.

 ## What the model does prove:
    * validity (every accepted transaction was authorised),
    * uniqueness and replay resistance (nullifier, tx_id, per-input),
    * chain-of-custody (every accepted input traces back to a
      legitimately created wallet note),
    * recipient consistency (Bob's view matches the chain's view),
    * KEM-layer secrecy: shared_secret leaks only via view_sk_B
      compromise,
    * AEAD-layer secrecy: amount_b leaks only if shared_secret
      was leaked (independent of how),
    * end-to-end secrecy: amount_b leaks only via view_sk_B
      compromise (composed from the above two).

  ## Scope limitations:
    * Liveness: once Spend consumes Alice's Wallet into Pending, the
      transaction must reach Accept or Alice's note is unrecoverable.
      Timeout / reclaim mechanisms are a natural extension.
    * Unlinkability and IND-CPA-style privacy (amount hiding in
      commit_b against a non-KEM-compromising adversary) are
      stronger properties not stated here. They would require
      game-based reasoning or explicit equational modelling of
      commitment hiding.

  Abstraction of zk_prove:

    zk_prove(tx_id, commit_a, commit_b) abstracts a proof
    system assumed to be sound, binding to all three public inputs,
    and non-malleable. It stands in for a proof of knowledge that:
      * commit_a opens to Alice's input note,
      * commit_b is the correct commitment for Bob,
      * the nullifier is correctly derived from Alice's spend key
        and her note's serial number,
      * balance conservation holds (discharged in Lean).

  Because zk_prove has no equations in this model, the Dolev-Yao
  adversary cannot construct a zk_prove term except by replay.

  Naming conventions:
    * spend_pk_A / spend_pk_B : h(spend_sk_X), the payment address
      (hash-derived, not a raw public key).
    * view_pk_B             : pk(view_sk_B), Bob's viewing public
      key used for asymmetric encryption of the shared secret.
    * commit_X              : note_commit(...), the chain-level
      commitment to a note.
    * salt_X                : blinding randomness in the commitment.
    * amount_X              : the note value.
    * serial_X              : chain-assigned tree index used as
      nullifier-derivation input.
    * shared_secret         : ephemeral secret Alice generates per
      transaction, encapsulated under view_pk_B.
    * ctxt                  : aenc(shared_secret, view_pk_B).
    * k / salt_b   : AEAD key / commitment-salt derived
      from shared_secret.
    * enc_tx_data           : AEAD-encrypted memo carrying
      (tx_id, token_id, amount_b) for Bob.
    * zkp              : zk_prove(...) term.
*/
