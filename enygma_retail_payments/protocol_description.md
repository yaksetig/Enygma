# Enygma Retail Payments Protocol

## System Entites
We assume a simple setting with three entities:
* Alice (the sender)
* Blockchain (the verification layer)
* Bob (the recipient)

  
```mermaid
flowchart LR
    A(["Alice"]) <-.-> C(["Blockchain"]) <-.-> B(["Bob"])
    
```

### Adversarial Model
The goal of the adversary $$\mathcal{A}$$ is twofold: break the privacy of the system and disrupt the activity of the system. We assume that the adversary ...


---
## Key Generation

In this step, each user generates two key pairs: a view key pair (i.e., ML-KEM) and a spend key pair (i.e., a hash-based key). 

First, the user generates an ML-KEM keypair. The purpose of this key is to allow for quantum-secure key agreements in the various transactions that take place.

$$
(pk^{\text{view}}, sk^{\text{view}}) \longleftarrow \mathrm{ML\text{-}KEM.KeyGen}()
$$

Each user also generates a spend keypair:

$$
(pk^{\text{spend}}, sk^{\text{spend}}) \longleftarrow \mathrm{Hash.KeyGen}()
$$

We envision a simple key pair where the secret key is a simple preimage and the public key is a hash digest of said preimage. 

$$
sk_A^{\text{spend}} \longleftarrow \\{ 0,1\\}^\lambda
$$

$$
pk_A^{\text{spend}} = \mathrm{H}(sk_A^{\text{spend}})
$$


---
## Registration

Upon completion of the key generation stage, each user registers both public keys. Therefore, each user publishes:

$$
(id, pk_i^{\text{view}}, pk_i^{\text{spend}})
$$

The output of this stage once different users register is a public-key record layer of the following style:

<div align="center">

| User | View Key | Spend Key |
|:----:|:--------:|:---------:|
| $A$ | $pk_A^{\text{view}}$ | $pk_A^{\text{spend}}$ |
| $B$ | $pk_B^{\text{view}}$ | $pk_B^{\text{spend}}$ |
| $\vdots$ | $\vdots$ | $\vdots$ |
| $Z$ | $pk_Z^{\text{view}}$ | $pk_Z^{\text{spend}}$ |

</div>

---



## Transaction Structure
We go over the case where Alice, who has an initial commitment $$C_{A}$$ sends Bob a payment. 
The goal of the protocol is for Alice to non-interactively have the ability to send funds to Bob. 

Each commitment is of the following form: 

$$ 
Commitment = H(pk^{\text{spend}}, salt, token_{id}, amount) 
$$

### Transaction Payload
Before proceeding with the description of the payment protocol, we higlight the tx structure of each private payment.

<div align="center">

| Ciphertext (ML-KEM) | Destination Commitment | Change Commitment | Ciphertext (AES) | Nullifier | $\pi$ |
|:-------------------:|:----------------------:|:-----------------:|:----------------:|:---------:|:-----:|

</div>

### Retrieving Destination Keys
Before initiating a transfer, Alice must know the public keys associated with Bob. Therefore, Alice retrieves the pair $(pk_B^{\text{view}}$, $pk_B^{\text{spend}})$. 

### Transaction Amount(s)
We assume that the commitment owned by Alice (i.e., $$C_{A}$$) contains a max amount $$v$$ that Alice can spend. 
In our protocol, the sender (i.e., Alice) always sends the full amount in every transaction. The corresponding amount to the recipient and the remaining to a "change commitment". 


We denote $$v_{1}$$ the amount being sent and $$v_{2}$$ the change amount. The amount $$v = v_{1} + v_{2}$$ is the amount owned by Alice in $$C_{A}$$.

### Token IDs
Each transfer is related to a specific token ID. We enforce this by having each commitment linked to a token ID. In fact, the token ID is one of the inputs to the commitment. 


---
## Transaction Creation

### Step 1 — ML-KEM Encapsulation

Alice initiates a post-quantum key agreement step and obtains a ciphertext and a shared secret $$ss$$. 

$$
(\mathrm{ctxt}, ss) \leftarrow \mathrm{ML\text{-}KEM.Encaps}(pk_B^{\text{view}})
$$

---

### Step 2 — Key Derivation
Upon generating the shared secret, Alice must derive two values: 

* a salt, which is used to mask/randomize the destination commitment
* a symmetric key, which is used to encrypt additional data that is appended to the transaction

We assume the existence of global system parameters: $$context_{k}$$ and $$context_{salt}$$, to be used as inputs to the Hash-based Key Derivation function to produce independent values to be used for encryption and randomness of the commitments. 

#### Deriving a symmetric key

$$
k = \mathrm{HKDF}(ss, context_{k}, len_{k})
$$

#### Deriving a salt

$$
\mathrm{salt_{B}} = \mathrm{HKDF}(ss, context_{salt}, len_{salt})
$$

---

### Step 3 — Commitment

Alice calculates the destination commitment (for Bob):

$$
\mathrm{Commitment_{B}}
= \mathrm{H}(pk_{B}^{\text{spend}} \parallel salt_{B} \parallel token_{id} \parallel v_{1})
$$

Alice calculates the change commitment (for Alice):

$$
\mathrm{Commitment_{A}}
= \mathrm{H}(pk_{A}^{\text{spend}} \parallel salt_{A} \parallel token_{id} \parallel v_{2})
$$

---

### Step 4 — Encrypt Transaction Data

$$
m = \mathrm{token\_id} \parallel \mathrm{amount}
$$

$$
\mathrm{ENC TX DATA} = \mathrm{AEAD.Enc}(k, m)
$$

---

### Step 5 — Nullifier

Let $\mathrm{index}$ be the spent note index:

$$
nf = \mathrm{HASH}(sk_A^{\text{spend}} \parallel \mathrm{leafIndex}_{A})
$$

---

### Step 6 — Submission

Alice submits the following payload:

<div align="center">

| ML-KEM.CTXT | $$\mathrm{Commitment_{B}}$$ | $$\mathrm{Commitment_{A}}$$ | AES-GCM-ENC(k, m, _) | $$\mathrm{nf}$$ | $\pi$ |
|:-----------:|:---------------------------:|:---------------------------:|:--------------------:|:---------------:|:-----:|

</div>

---

## 5. Transaction Processing (Bob)

Given $(\mathrm{ctxt}, \mathrm{commit}, \mathrm{enc})$:

---

### Step 1 — ML-KEM Decapsulation

$$
ss_B \leftarrow \mathrm{ML\text{-}KEM.Decaps}(sk_B^{\text{view}}, \mathrm{ctxt})
$$

---

### Step 2 — Key Derivation

$$
(\mathrm{salt}', k') = \mathrm{HKDF}(\mathrm{nil}, ss_B, \mathrm{info})
$$

---

### Step 3 — Decrypt

Since this is an authenticated encryption (with associated data) scheme, Bob will know if the used key is correct as the authentication component of the cipher will succeed. 

$$
(token_{id}, v_{1}) = \mathrm{AEAD.Dec}(k', \mathrm{enc}, \mathrm{ctxt})
$$

---

### Step 4 — Recompute Commitment

$$
\mathrm{commit}' = \mathrm{Commit}(pk_B^{\text{spend}}, \mathrm{salt}', \mathrm{token\_id}, v')
$$

Accept iff:

$$
\mathrm{commit}' = \mathrm{commit}
$$

---

## 7. Security Goals

### Confidentiality

$$
\mathrm{amount}, \mathrm{salt} \text{ remain hidden without } sk_B^{\text{view}}
$$

---

### Key Privacy

$$
sk_A^{\text{view}},\; sk_B^{\text{view}},\; sk_A^{\text{spend}},\; sk_B^{\text{spend}} \text{ remain secret}
$$

---

### Binding

$$
\mathrm{commit} \text{ binds } (pk_B^{\text{spend}}, \mathrm{salt}, \mathrm{token\_id}, \mathrm{amount})
$$

---

### Nullifier

$$
nf = \mathrm{HASH}(sk_A^{\text{spend}} \parallel \mathrm{index})
$$

- prevents double-spend  
- unlinkable to $pk_A^{\text{spend}}$

---

## 8. Notes

- View keys are **ML-KEM keys (not Diffie–Hellman)**
- Single-input spend (no anonymity set)
- No balance / sum constraints
- Recipient detection is implicit via decapsulation + AEAD check
- ZK proof is omitted and must enforce:
  - knowledge of $sk_A^{\text{spend}}$
  - correct nullifier construction
