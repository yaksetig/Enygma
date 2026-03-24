
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

    note over Alice: Generate new ('encrypted') salt for Bob:<br><br>salt_B, ctxt = ML-KEM.Encapsulate(view_pk_B)

    note over Alice: Set TX DATA: <br><br>m = (token_id || amount)<br><br>k = salt_B

    note over Alice: Encrypt TX Data: <br><br> ENC_TX_DATA = AES-GCM-ENC(k, m)

    note over Alice: Create destination commitment:<br><br>COMMIT_B = H(spend_pk_B, salt_B, amount, token_id)

    note over Alice: Create zero-knowledge proof (π)

    note over Alice: Create nullifier for tx (nf)

    Alice->>Chain: < π, CTXT, COMMIT_B, ENC_TX_DATA, nf >

    note over Chain: Verify ZK Proof

    alt Verify(π) = TRUE
        note over Chain: Mark 'nf' as spent
        Chain ->> Alice: TX OK


    else Verify(π) = FALSE
      note over Chain: Reject TX
      Chain ->> Alice: TX ERROR

    end

    Chain ->> Bob: < CTXT, COMMIT_B, ENC_TX_DATA >

    note over Bob: Decapsulate CTXT and<br>obtain symmetric key:<br><br> k = salt_B


    note over Bob: Decrypt ENC_TX_DATA<br>(using symmetric key k)<br><br>obtain token_id & amount

    note over Bob: Obtain commitment<br><br>C = H(spend_pk_B, salt_B, token_id, amount)
    note over Bob: Check if commitments match: <br><br>C == COMMIT_B
```

### Additional Remark(s)
Alice was able to send funds to Bob.<br><br>Only Bob can spend the received commitment<br><br>The protocol does not require any interaction from Bob.
