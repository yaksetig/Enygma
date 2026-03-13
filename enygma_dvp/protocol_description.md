# Protocol Description


## Notation


In Enygma DvP, the commitments have the following form:

$$C = Hash(pk^{spend} | salt | token_{ID} | amount)$$

To spend the commitment, the user proves in zero-knowledge that they know the secret spend key associated with this commitment, and publish a nullifier that spends the corresponding commitment. 

## 1 - System Setup
TBD

## 2 - Key Generation
Each privacy node generates two keypairs: one to spend funds, and one to 'view' transactions. Concretely: 

* Privacy node A generates an [ML-KEM](https://nvlpubs.nist.gov/nistpubs/FIPS/NIST.FIPS.203.pdf) (view) keypair and obtains $$(sk_{A}^{view}, pk_{A}^{view})$$

* Privacy node A generates a simple hash-based (spend) keypair and obtains $$(sk_{A}^{spend}, pk_{A}^{spend})$$.
  *  $$sk_{A}^{spend} \longleftarrow \\\{{0, 1\\\}}^{256}$$
  *  $$pk_{A}^{spend} = Hash(sk_{A}^{spend})$$
 
The goal here is to have segregation of functionalities with each keypair. 

* To spend, the user proves in zero-knowledge that they know a secret key $$sk^{spend}$$ corresponding to one public key $$pk^{spend}$$ in an anonymity set of size $$k$$. We note that the hashing used in this step is ZK-friendly (i.e., Poseidon).
* The view key pair is used to decrypt the values that are inserted into the received commitments. 

## Private Issuance

To spend the funds, the recipient must be able to open the commitment. Concretely, the user must know the spend key pair, the salt, the token ID, and the amount. Therefore, we require a mechanism that allows the issuer to share the salt with the recipient. An initial approach could have the recipient communicate with the issuer in advance and send the already-formed commitment. The issuer, then simply performs a mint directly to that commitment. This is possible, but not elegant and makes the payment extremely interactive. A user should simply be able to privately send funds to the other user right away. 

Issuer:

* generates a random salt:
   * $$salt \longleftarrow \lbrace0, 1\rbrace^{\lambda}$$

## Protocol Flow

```mermaid

---
config:
  theme: redux
---
sequenceDiagram
    autonumber
    participant Alice
    participant Chain as Blockchain
    participant Bob

    note over Chain: Runs a DvP<br>Smart Contract

    Note over Alice: Owns an existing note<br>C_A = Commit(spend_pkA, saltA, amount=5, token_id=10)<br>(represents 5 USDT)
    Note over Bob: Owns an existing note<br>C_B = Commit(spend_pkB, saltB, amount=1, token_id=25)<br>(represents 1 concert ticket)

    note over Alice, Bob: < Agree on trade parameters. Swap 5 USDT (token_id = 10) for 1 concert ticket(token_id = 25) > 

    note over Alice: Obtain <br>(saltB, ciphertext_I) = Encapsulate(view_pkB)

    note over Alice: Generate new salt : salt*
    note over Alice: Create new commitment (Bob will send funds here): <br>Commitment_A = Hash(spend_pkA, salt*, amount=1, token_id=25)
    
    note over Alice: Generate new (revert) salt : newSaltA
    note over Alice: Create revert commitment (for Alice if Bob doesn't send): <br>C'_A = Hash(spend_pkA, newSaltA, amount=5, token_id=10)

    

    note over Alice: Create message:<br> m = (token_id || amount || salt* )
    note over Alice: Set<br> k = saltB

    note over Alice: Calculate<br> ciphertext_II = ENC_AEAD(k, m)

    note over Alice: Commitment_B = Hash(spend_pkB, saltB, amount, token_id)

    note over Alice: Create DvP_id = HASH(Commitment_B, Commitment_A)

    Alice->>Chain: nullifier_A, ZKP, ciphertext_I, Commitment_B, DvP_id, ciphertext_II, C'_A,

    Note over Chain: Mark nullifier_A as 'locked'
    Note over Chain: Mark TX with id = DvP_id as 'pending'

    Chain-->>Bob: New tx (& payloads)

    note over Bob: Obtain<br>salt' = Decapsulate(view_skB, ciphertext_I)
    note over Bob: Set<br> k' = salt'
    note over Bob: Attempt Decrypt<br>(ok, m) = DEC_AEAD(k', ciphertext_II)

    alt Decryption Ok
        Note over Bob: Parse m -> (token_id, amount, salt*)
        Note over Bob: _commitment_B = Hash(spend_pkB, salt', amount, token_id)
        Note over Bob: Compare _commitment_B == commitment_B
        
        Note over Bob: If equal, funds belong to Bob

        Note over Bob: Check if Commitment_A is well-formed

        Bob ->> Chain: tx_id, nullifier_B, ZKP, Commitment_A

        alt TX OK
            Note over Chain: Calculate tx_id = HASH()
            Note over Chain: Mark nullifier_A & nullifier_B as used 
            Note over Chain: Mark C_A & C_B as spent

            Note over Chain: Insert Commitment_A and Commitment_B in tree
        else Timeout
            Note over Chain: Move TX_id = DvP_id from 'pending' to 'reverted'
            Note over Chain: Mark nullifier_A as used 
            Note over Chain: Mark C_A as spent
            Note over Chain: Insert C'_A into tree
        end

    else Decryption Fail
        Note over Bob: Payload not intended for Bob
    end





```
