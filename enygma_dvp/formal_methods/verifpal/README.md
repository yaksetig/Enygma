# Verifpal Model

In this folder, we modeled the Enygma DvP protocol  using Verifpal. We note that the tool found no attacks against the protocol. 

## Modelling ML-KEM
We use the PKE_ENC (i.e., public-key encryption) primitive to model the ML-KEM Encapsulate and PKE_DEC (i.e., public-key decryption) to model the ML-KEM Decapsulate. We believe this is reasonable given that from a black box perspective both are functionally the same. 
