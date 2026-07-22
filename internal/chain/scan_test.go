package chain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type chainInfoRPCStub struct {
	response any
	err      error
}

func (s chainInfoRPCStub) Call(_ context.Context, method string, params any, out any) error {
	if s.err != nil {
		return s.err
	}
	if method != "getblockchaininfo" {
		return errors.New("unexpected method")
	}
	if params != nil {
		return errors.New("unexpected params")
	}
	b, err := json.Marshal(s.response)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestGetChainInfoUsesNextBlockConsensusBranch(t *testing.T) {
	t.Parallel()

	info, err := GetChainInfo(context.Background(), chainInfoRPCStub{response: map[string]any{
		"chain":  "regtest",
		"blocks": 123,
		"consensus": map[string]any{
			"chaintip":  "deadbeef",
			"nextblock": "c8e71055",
		},
	}})
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.Chain != "regtest" || info.Height != 123 {
		t.Fatalf("chain info=%+v", info)
	}
	if info.BranchID != 0xc8e71055 {
		t.Fatalf("branch id=%08x want c8e71055", info.BranchID)
	}
	if info.BranchID == 0xdeadbeef {
		t.Fatal("selected chaintip branch instead of nextblock")
	}
}

func TestGetChainInfoRequiresValidNextBlockConsensusBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		nextBlock any
		wantError string
	}{
		{name: "missing", wantError: "missing consensus.nextblock"},
		{name: "empty", nextBlock: "  ", wantError: "missing consensus.nextblock"},
		{name: "not hex", nextBlock: "not-hex", wantError: "invalid consensus.nextblock"},
		{name: "too large", nextBlock: "100000000", wantError: "invalid consensus.nextblock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			consensus := map[string]any{"chaintip": "deadbeef"}
			if tt.nextBlock != nil {
				consensus["nextblock"] = tt.nextBlock
			}
			_, err := GetChainInfo(context.Background(), chainInfoRPCStub{response: map[string]any{
				"chain":     "regtest",
				"blocks":    123,
				"consensus": consensus,
			}})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}
