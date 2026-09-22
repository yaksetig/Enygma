#!/usr/bin/env python3
"""Live-stack smoke test for the Enygma retail payments demo.

Drives the demo's HTTP API against a running stack and asserts the protocol
actually did what the UI claims: eight parties registered in order with the keys
the chain holds, each privacy mode producing the bitmap it promises, a payment
that proves, relays, mines and publishes a tag, a decoy scan that fails where the
recipient's succeeds, and repeat payments spending evolved notes.

It needs a clean chain, so run it right after starting the launcher:

    cd demo && bash run.sh        # terminal 1
    python3 demo/smoke.py         # terminal 2

Not part of CI: it requires the whole live stack (chain, prover, relayer).
"""
import json, sys, urllib.request, urllib.error

BASE = "http://127.0.0.1:9091"
FAILURES = []
STEPS = 0

def call(path, body=None, timeout=300):
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(BASE + path, data=data,
                                 headers={"Content-Type": "application/json"},
                                 method="POST" if body is not None else "GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())

def check(name, cond, detail=""):
    global STEPS
    STEPS += 1
    if cond:
        print(f"  ok   {name}")
    else:
        print(f"  FAIL {name} :: {detail}")
        FAILURES.append(name)

def bits_of(s, i):
    return s[i] == "1"

print("== 1. registration ==")
st, d = call("/api/register", {})
check("register returns 200", st == 200, d.get("error"))
if st != 200:
    sys.exit(1)
check("registered flag set", d["registered"] is True)
names = [p["name"] for p in d["parties"]]
check("eight rows in order", names == ["Alice","Bob","Charlie","Diana","Evan","Fatima","Grace","Henry"], names)
check("indices are 0..7 in order", [p["index"] for p in d["parties"]] == list(range(8)))
check("all registered", all(p["registered"] for p in d["parties"]))
check("all have on-chain pk_spend", all(p["pkSpend"].startswith("0x") and len(p["pkSpend"]) > 10 for p in d["parties"]))
check("all pk_view are 1184 bytes", all(p["pkViewLen"] == 1184 for p in d["parties"]))
check("distinct pk_spend per party", len({p["pkSpend"] for p in d["parties"]}) == 8)
ctrl = [p for p in d["parties"] if p["controllable"]]
check("three controllable parties", [p["name"] for p in ctrl] == ["Alice","Bob","Charlie"])
check("each controllable seeded with 100", all(p["balance"] == 100 for p in ctrl),
      [(p["name"], p["balance"]) for p in ctrl])
check("three commitment leaves", d["tree"]["leafCount"] == 3, d["tree"]["leafCount"])
root_after_seed = d["tree"]["root"]
check("tree has a root", bool(root_after_seed))

print("== 2. actor secrets are scoped ==")
st, d = call("/api/actor", {"index": 0})
check("act as Alice", st == 200, d.get("error"))
sec = d["secrets"]
check("Alice secrets returned", sec["name"] == "Alice" and sec["skSpend"].startswith("0x") and sec["skView"].startswith("0x"))
alice_sk = sec["skSpend"]
state = d["state"]
check("state carries only the actor's secrets", state["actorSecrets"]["index"] == 0)
check("state json has no other party's secret", "skSpend" not in json.dumps(state["parties"]))
st, d = call("/api/actor", {"index": 1})
check("act as Bob", st == 200, d.get("error"))
check("Bob's secrets differ from Alice's", d["secrets"]["skSpend"] != alice_sk)
st, d = call("/api/actor", {"index": 3})
check("cannot act as a background user", st != 200 and "background" in d.get("error", ""), d)

print("== 3. privacy modes ==")
call("/api/actor", {"index": 0})
expect = {"none": 1, "subset": 3, "full": 8}
for mode, want in expect.items():
    st, d = call("/api/channels/preview", {"recipient": 1, "mode": mode, "excluded": []})
    check(f"{mode}: preview ok", st == 200, d.get("error"))
    if st != 200: continue
    pv = d["preview"]
    check(f"{mode}: {want} candidate bits", len(pv["candidates"]) == want, pv["bits"])
    check(f"{mode}: recipient bit set", bits_of(pv["bits"], 1), pv["bits"])
# Rift with exclusions
st, d = call("/api/channels/preview", {"recipient": 1, "mode": "rift", "excluded": [0, 4, 5]})
check("rift: preview ok", st == 200, d.get("error"))
pv = d["preview"]
check("rift: 5 candidate bits", len(pv["candidates"]) == 5, pv["bits"])
check("rift: recipient still set", bits_of(pv["bits"], 1), pv["bits"])
check("rift: exclusions cleared", not any(bits_of(pv["bits"], i) for i in (0, 4, 5)), pv["bits"])
st, d = call("/api/channels/preview", {"recipient": 1, "mode": "rift", "excluded": [1, 4]})
check("rift: recipient cannot be excluded", d["preview"] is not None and bits_of(d["preview"]["bits"], 1))

print("== 4. stale preview is refused ==")
st, d = call("/api/channels/open", {"previewId": "pv-does-not-exist"})
check("stale preview rejected", st != 200 and "stale" in d.get("error", ""), d)

print("== 5. establish Alice -> Bob (subset) ==")
st, d = call("/api/channels/preview", {"recipient": 1, "mode": "subset", "excluded": []})
pvid = d["preview"]["id"]; pvbits = d["preview"]["bits"]
st, d = call("/api/channels/open", {"previewId": pvid})
check("channel published", st == 200, d.get("error"))
ch = [c for c in d["channels"] if c["sender"] == 0 and c["recipient"] == 1]
check("channel recorded for the pair", len(ch) == 1)
if ch:
    c = ch[0]
    check("published bitmap matches the preview", c["bits"] == pvbits, (c["bits"], pvbits))
    check("on-chain bitmap matches what was sent", c["bitmapMatches"] is True, (c["onChainBits"], c["bits"]))
    check("channel has a tx and block", bool(c["txHash"]) and c["block"] > 0)
    check("recipient bit set on-chain", bits_of(c["onChainBits"], 1), c["onChainBits"])
    subset_bits = c["onChainBits"]

print("== 6. payment guards ==")
st, d = call("/api/payments", {"recipient": 1, "amount": 0})
check("zero amount refused", st != 200 and "positive" in d.get("error", ""), d)
st, d = call("/api/payments", {"recipient": 1, "amount": 5000})
check("oversized amount refused", st != 200 and "no single unspent note" in d.get("error", ""), d)
st, d = call("/api/payments", {"recipient": 2, "amount": 10})
check("payment without a channel refused", st != 200 and "no channel" in d.get("error", ""), d)
st, d = call("/api/payments", {"recipient": 0, "amount": 10})
check("self-payment refused", st != 200 and "themselves" in d.get("error", ""), d)

print("== 7. Alice -> Bob payment ==")
st, d = call("/api/payments", {"recipient": 1, "amount": 30})
check("payment succeeded", st == 200, d.get("error"))
if st != 200:
    print("stopping: payment failed"); sys.exit(1)
pay = d["payments"][-1]
check("payment tx mined", bool(pay["payTxHash"]) and pay["payBlock"] > 0)
check("nullifier published", pay["nullifier"].startswith("0x") and len(pay["nullifier"]) > 10)
check("proof was generated", pay["proofMs"] > 0)
check("ML-KEM capsule is 1088 B", pay["cipherTextLen"] == 1088, pay["cipherTextLen"])
check("both output commitments present", pay["destCommitment"] != pay["changeCommitment"] and pay["destCommitment"].startswith("0x"))
check("tag published in its own tx", bool(pay["tagTxHash"]) and pay["tagTxHash"] != pay["payTxHash"])
check("one 32-byte tag published", len(pay["publishedTag"]) == 66, pay["publishedTag"])
check("tree grew by two leaves", d["tree"]["leafCount"] == 5, d["tree"]["leafCount"])
check("merkle root changed", d["tree"]["root"] != root_after_seed)
check("Alice keeps 70 spendable", [p for p in d["parties"] if p["index"] == 0][0]["balance"] == 70,
      [p["balance"] for p in d["parties"]])
bob = [p for p in d["parties"] if p["index"] == 1][0]
check("Bob's note is unscanned, not spendable", bob["balance"] == 100 and bob["pending"] == 30, (bob["balance"], bob["pending"]))
root_after_pay1 = d["tree"]["root"]

print("== 8. decoy vs true recipient ==")
# Charlie is index 2. Only meaningful if the subset bitmap actually includes him.
call("/api/actor", {"index": 2})
st, d = call("/api/scan", {})
check("Charlie scan ok", st == 200, d.get("error"))
sc = d["lastScan"]
outcomes = {c["channelIdx"]: c["outcome"] for c in sc["channels"]}
charlie_in_set = bits_of(subset_bits, 2)
if charlie_in_set:
    check("Charlie is a decoy (AEAD fails)", outcomes.get(0) == "decoy", sc)
else:
    check("Charlie skips the channel (bit clear)", outcomes.get(0) == "skipped", sc)
check("Charlie recovered no notes", all(not c.get("notes") for c in sc["channels"]), sc)
check("Charlie's balance unchanged", [p for p in d["parties"] if p["index"] == 2][0]["balance"] == 100)

call("/api/actor", {"index": 1})
st, d = call("/api/scan", {})
check("Bob scan ok", st == 200, d.get("error"))
sc = d["lastScan"]
matched = [c for c in sc["channels"] if c["outcome"] == "matched"]
check("Bob matched the channel", len(matched) == 1, sc)
notes = matched[0].get("notes") if matched else []
check("Bob decrypted one note", len(notes or []) == 1, notes)
if notes:
    check("note amount is 30", notes[0]["amount"] == "30", notes[0])
    check("recomputed commitment verified", notes[0]["verified"] is True, notes[0])
    check("commitment equals the on-chain destination", notes[0]["commitment"] == pay["destCommitment"],
          (notes[0]["commitment"], pay["destCommitment"]))
bob = [p for p in d["parties"] if p["index"] == 1][0]
check("Bob now holds 130 spendable", bob["balance"] == 130, bob["balance"])
check("nothing left pending for Bob", bob["pending"] == 0, bob["pending"])

print("== 9. repeat payment on an evolved note ==")
# Bob pays Charlie 45 out of the 30-token note he just discovered plus his seed
# note; the 100 note is the smallest sufficient one.
st, d = call("/api/channels/preview", {"recipient": 2, "mode": "full", "excluded": []})
check("Bob -> Charlie preview", st == 200, d.get("error"))
st, d = call("/api/channels/open", {"previewId": d["preview"]["id"]})
check("Bob -> Charlie channel opened", st == 200, d.get("error"))
st, d = call("/api/payments", {"recipient": 2, "amount": 45})
check("second payment succeeded", st == 200, d.get("error"))
if st == 200:
    pay2 = d["payments"][-1]
    check("second payment is a new tx", pay2["payTxHash"] != pay["payTxHash"])
    check("second payment spent the 100 note (55 change)", pay2["change"] == "55", pay2["change"])
    check("tree grew to 7 leaves", d["tree"]["leafCount"] == 7, d["tree"]["leafCount"])
    check("root changed again", d["tree"]["root"] != root_after_pay1)
    check("Bob holds 85 after paying 45", [p for p in d["parties"] if p["index"] == 1][0]["balance"] == 85,
          [p["balance"] for p in d["parties"]])

    # Now spend the note Bob only learned about through the tag scan.
    st, d = call("/api/payments", {"recipient": 2, "amount": 30})
    check("spending the tag-discovered note works", st == 200, d.get("error"))
    if st == 200:
        check("Bob down to 55", [p for p in d["parties"] if p["index"] == 1][0]["balance"] == 55,
              [p["balance"] for p in d["parties"]])

    call("/api/actor", {"index": 2})
    st, d = call("/api/scan", {})
    check("Charlie scan after being paid", st == 200, d.get("error"))
    got = sum(len(c.get("notes") or []) for c in d["lastScan"]["channels"])
    check("Charlie decrypted both notes", got == 2, d["lastScan"])
    check("Charlie holds 175", [p for p in d["parties"] if p["index"] == 2][0]["balance"] == 175,
          [p["balance"] for p in d["parties"]])

print("== 10. state hygiene ==")
st, d = call("/api/state")
check("state is 200", st == 200)
blob = json.dumps(d)
check("no private key material in public state", "skSpend" not in json.dumps(d["parties"]) and "sharedSecret" not in blob)
check("health reports all three services", d["health"] == {"chain": True, "gnark": True, "relayer": True}, d["health"])

print()
print(f"{STEPS - len(FAILURES)}/{STEPS} checks passed")
if FAILURES:
    print("FAILED: " + ", ".join(FAILURES))
    sys.exit(1)
print("ALL GREEN")
