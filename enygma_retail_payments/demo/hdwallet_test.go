package main

import "testing"

func TestAccountKeysMatchConfig(t *testing.T) {
	// enygmapayment.config.json accounts[0..2] — the addresses the deploy and
	// relayer scripts already use. If derivation drifts, these fail loudly.
	want := []string{
		"0x0F1013e0e46B97144b25b3131668EF99858BD8D0",
		"0xD2C3b34Abae5664986C8cf0F14d1D434Ac894768",
		"0x9E0B331577BB37420231DAc6D199FCb4c7092B87",
	}
	for i, w := range want {
		priv, addr, err := accountKey(i)
		if err != nil {
			t.Fatalf("accountKey(%d): %v", i, err)
		}
		if addr.Hex() != w {
			t.Errorf("account %d = %s (priv %s), want %s", i, addr.Hex(), priv, w)
		}
	}
	for i := 3; i < 10; i++ {
		priv, addr, err := accountKey(i)
		if err != nil {
			t.Fatalf("accountKey(%d): %v", i, err)
		}
		t.Logf("account %d  %s  %s", i, addr.Hex(), priv)
	}
}
