package txbuild

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Abdullah1738/juno-txbuild/internal/chain"
)

type chainContextRPCStub struct {
	response any
	err      error
}

func (s chainContextRPCStub) Call(_ context.Context, method string, params any, out any) error {
	if s.err != nil {
		return s.err
	}
	if method != "getblockchaininfo" {
		return errors.New("unexpected method")
	}
	if params != nil {
		return errors.New("unexpected params")
	}
	raw, err := json.Marshal(s.response)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func TestVerifyChainContextRejectsRollbackNetworkChangeAndConsensusUpgrade(t *testing.T) {
	t.Parallel()

	const branchID = uint32(0xc8e71055)
	expected := chain.ChainInfo{Chain: "regtest", Height: 100, BranchID: branchID}
	snapshot := scannerAnchorSnapshot{height: 100, hash: "anchor-hash"}

	tests := []struct {
		name      string
		chain     string
		height    int64
		branchID  uint32
		expiry    uint32
		wantError string
	}{
		{name: "stable", chain: "regtest", height: 100, branchID: branchID, expiry: 140},
		{name: "advanced in same epoch", chain: "regtest", height: 102, branchID: branchID, expiry: 140},
		{name: "tip rollback", chain: "regtest", height: 99, branchID: branchID, expiry: 140, wantError: "tip moved behind"},
		{name: "network change", chain: "main", height: 100, branchID: branchID, expiry: 140, wantError: "network changed"},
		{name: "consensus upgrade", chain: "regtest", height: 101, branchID: 0xdeadbeef, expiry: 140, wantError: "consensus branch changed"},
		{name: "expiry window exhausted", chain: "regtest", height: 137, branchID: branchID, expiry: 140, wantError: "expiry became too close"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifyChainContext(context.Background(), chainContextRPCStub{response: map[string]any{
				"chain":  tt.chain,
				"blocks": tt.height,
				"consensus": map[string]any{
					"nextblock": branchHex(tt.branchID),
				},
			}}, expected, snapshot, tt.expiry)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("verifyChainContext: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestVerifyChainContextPropagatesRPCAndConsensusErrors(t *testing.T) {
	t.Parallel()

	expected := chain.ChainInfo{Chain: "regtest", Height: 100, BranchID: 0xc8e71055}
	snapshot := scannerAnchorSnapshot{height: 100, hash: "anchor-hash"}

	if err := verifyChainContext(context.Background(), chainContextRPCStub{err: errors.New("rpc unavailable")}, expected, snapshot, 140); err == nil || !strings.Contains(err.Error(), "rpc unavailable") {
		t.Fatalf("rpc error=%v", err)
	}
	if err := verifyChainContext(context.Background(), chainContextRPCStub{response: map[string]any{
		"chain":     "regtest",
		"blocks":    int64(100),
		"consensus": map[string]any{},
	}}, expected, snapshot, 140); err == nil || !strings.Contains(err.Error(), "missing consensus.nextblock") {
		t.Fatalf("consensus error=%v", err)
	}
}

func branchHex(branchID uint32) string {
	const digits = "0123456789abcdef"
	var out [8]byte
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = digits[branchID&0xf]
		branchID >>= 4
	}
	return string(out[:])
}
