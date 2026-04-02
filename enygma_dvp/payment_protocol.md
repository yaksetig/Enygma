
## Problem Statement
A commitment is of the form Commit = H(spend_pk, salt, token_id, amount).

How can Alice send funds to a commitment only Bob can open in a non-interactive manner? 

## Protocol Flow
```mermaid

---
config:
  theme: redux
  look: handDrawn
---
sequenceDiagram
    autonumber

    participant Alice
    participant Chain as Blockchain
    participant Bob

    note over Alice: Has commmitment<br><br>Commitment_A = H(spend_pkA, saltA, amount_1, token_id_1)

    note over Bob: Has commitment<br><br>Commitment_B = H(spend_pkB, saltB, amount_2, token_id_2)

    note over Chain: runs DvP Smart Contract

    note over Alice, Bob: DvP can now start

    note over Alice: Alice initiates the DvP
    

    rect rgb(191, 223, 255)

        note left of Alice: Create TX payload for Bob

        note over Alice: Generate new ('encrypted') salt:<br><br>ss_B, CTXT = ML-KEM.Encapsulate(view_pk)

        note over Alice: Set TX DATA: <br><br>m = (token_id || amount)<br><br>k = HKDF(ss_B, "encryption key")

        note over Alice: Encrypt TX Data: <br><br> ENC_TX_DATA = AES-GCM-ENC(k, m)
        
        note over Alice: Derive salt_B: <br><br> salt_B = HKDF(ss_B, "Bob salt")

        note over Alice: Create destination commitment:<br><br>COMMIT_B = H(spend_pk_B, salt_B, amount_1, token_id_1)

    end     
    

    

    rect rgb(191, 223, 255)

        note left of Alice: Create the ZKP for the transaction

        note over Alice: Create nullifier for tx: <br><br>nf_A = H(spend_sk_A, leafIndex_A)
        
        note over Alice: Create zero-knowledge proof (π_A):<br><br> - "I know the spend secret key for this commit"<br><br> - "This nullifier is well-formed"<br><br>- "The revert commit has the same amount and token_id as commit I'm spending"<br><br>-"The destination (COMMIT_B) has the same token_id_1 and amount_1 as the commit I'm spending"<br><br>-"I know a merkle path that proves that the commitment I'm spending is in the tree"

    end


    Alice->>Chain: < π_A, CTXT, COMMIT_B, ENC_TX_DATA, nf_A>

    

    note over Chain: Check if nf_A<br>has been spent
    note over Chain: Verify ZK Proof


    alt (Verify(π_A) = TRUE) && (nf_A NOT MARKED AS SPENT)

        note over Chain: Mark nf_A as spent
        Chain ->> Alice: TX OK


    else (Verify(π_A) = FALSE) || (nf_A IS MARKED AS SPENT) 
      note over Chain: Reject TX
      Chain ->> Alice: TX ERROR

    end


    Chain ->> Bob: < CTXT, COMMIT_B, ENC_TX_DATA, COMMIT_A >

    note over Bob: Decapsulate CTXT and<br>obtain ss_B


    note over Bob: Obtain symmetric key:<br><br> k = HKDF(ss_B, "encryption key")

    note over Bob: Obtain salt:<br><br> salt_B = HKDF(ss_B, "Bob salt")


    note over Bob: Decrypt ENC_TX_DATA<br>(using symmetric key k)<br><br>obtain token_id_1 & amount_1

    note over Bob: Obtain commitment<br><br>C = H(spend_pk_B, salt_B, amount_1, token_id_1)
    note over Bob: Check if commitments match: <br><br>C == COMMIT_B





   
```

### Additional Remark(s)
Alice was able to send funds to Bob.<br><br>Only Bob can spend the received commitment<br><br>The protocol does not require any interaction from Bob.
