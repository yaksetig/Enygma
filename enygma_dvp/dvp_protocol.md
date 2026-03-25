``` mermaid
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

    rect rgb(191, 223, 255)

    note left of Alice: 1. Create TX payload for Bob
    note over Alice: Generate new ('encrypted') salt:<br><br>salt_B, ctxt = ML-KEM.Encapsulate(view_pk)

    note over Alice: Set TX DATA: <br><br>m = (token_id || amount)<br><br>k = salt_B

    note over Alice: Encrypt TX Data: <br><br> ENC_TX_DATA = AES-GCM-ENC(k, m)

    note over Alice: Create destination commitment:<br><br>COMMIT_B = H(spend_pk_B, salt_B, amount, token_id)
    
    note over Alice: Generate new salt:<br><br>salt_A

    note over Alice: Create (self) destination commitment:<br><br>COMMIT_A = H(spend_pk_A, salt_A, amount, token_id)

    end     

    rect rgb(191, 223, 255)

    note left of Alice: Creation of Revert Commitment<br>(if TX fails, should revert to new commit)

    note over Alice: Generate new (revert) salt:<br><br>revert_salt_A
    note over Alice: Create revert commitment:<br><br>REVERT_COMMIT_A = H(spend_pk_A, revert_salt_A, amount, token_id)

    end

    rect rgb(191, 223, 255)

    note left of Alice: Finalize TX Process
    note over Alice: Create zero-knowledge proof (π):<br><br> - "I know the spend sk for this commit"<br><br> - "This nullifier is well-formed"<br><br>- "The revert commit has the same amount and token_id as commit I'm spending"<br><br>- "I can open the (self) destination commitment (COMMIT_A)"

    %% note left of Alice: Proof that: <br> - I know the spend sk for the commit being spent

    note over Alice: Create nullifier for tx: <br><br>nf = H(...)

    end


    Alice->>Chain: < π, CTXT, COMMIT_B, ENC_TX_DATA, COMMIT_A, REVERT_COMMIT_A, nf >

    note over Chain: Verify ZK Proof

    alt Verify(π) = TRUE
        note over Chain: Mark 'nf' as locked
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


    alt TX On Time

      Bob->>Chain: < π, CTXT, COMMIT_A, ENC_TX_DATA, nf >

      note over Chain: Mark nf_A as spent
      note over Chain: Mark nf_B as spent
      note over Chain: Insert COMMIT_A into tree
      note over Chain: Insert COMMIT_B into tree
      
      note over Chain: Mark DvP TX as completed


    else TX Timeout
      note over Chain: Mark nf_A as spent
      note over Chain: Insert REVERT_COMMIT_A into tree
      note over Chain: Mark DvP TX as failed (i.e., timeout)

      Chain ->> Alice: TX Reverted
    end

```
