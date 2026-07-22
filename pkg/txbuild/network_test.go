package txbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/types"
)

func TestResolveCoinType(t *testing.T) {
	tests := []struct {
		name       string
		chain      string
		configured uint32
		want       uint32
		wantError  string
	}{
		{name: "infer mainnet", chain: "main", want: mainnetCoinType},
		{name: "infer testnet", chain: "test", want: testnetCoinType},
		{name: "infer regtest", chain: "regtest", want: regtestCoinType},
		{name: "matching explicit mainnet", chain: "main", configured: mainnetCoinType, want: mainnetCoinType},
		{name: "matching explicit testnet", chain: "test", configured: testnetCoinType, want: testnetCoinType},
		{name: "matching explicit regtest", chain: "regtest", configured: regtestCoinType, want: regtestCoinType},
		{name: "mainnet mismatch", chain: "main", configured: regtestCoinType, wantError: "does not match chain"},
		{name: "testnet mismatch", chain: "test", configured: mainnetCoinType, wantError: "does not match chain"},
		{name: "regtest mismatch", chain: "regtest", configured: testnetCoinType, wantError: "does not match chain"},
		{name: "unknown inferred", chain: "unknown", wantError: "unknown chain"},
		{name: "unknown explicit", chain: "unknown", configured: mainnetCoinType, wantError: "unknown chain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCoinType(tt.chain, tt.configured)
			if tt.wantError != "" {
				assertCodedError(t, err, types.ErrCodeInvalidRequest, tt.wantError)
				return
			}
			if err != nil {
				t.Fatalf("resolveCoinType: %v", err)
			}
			if got != tt.want {
				t.Fatalf("coin type=%d want %d", got, tt.want)
			}
		})
	}
}

func TestResolveCoinTypeMismatchReportsExpectedValue(t *testing.T) {
	_, err := resolveCoinType("regtest", mainnetCoinType)
	assertCodedError(t, err, types.ErrCodeInvalidRequest, fmt.Sprintf("expected %d", regtestCoinType))
}

func TestTxPlanSchemaBindsSupportedNetworks(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "txplan.v0.schema.json"))
	if err != nil {
		t.Fatalf("read txplan schema: %v", err)
	}

	var schema struct {
		Properties struct {
			CoinType struct {
				Enum []uint32 `json:"enum"`
			} `json:"coin_type"`
			Chain struct {
				Enum []string `json:"enum"`
			} `json:"chain"`
		} `json:"properties"`
		AllOf []struct {
			OneOf []struct {
				Properties struct {
					CoinType struct {
						Const *uint32 `json:"const"`
					} `json:"coin_type"`
					Chain struct {
						Const *string  `json:"const"`
						Enum  []string `json:"enum"`
					} `json:"chain"`
				} `json:"properties"`
			} `json:"oneOf"`
		} `json:"allOf"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("decode txplan schema: %v", err)
	}

	wantCoinTypes := []uint32{mainnetCoinType, testnetCoinType, regtestCoinType}
	if !reflect.DeepEqual(schema.Properties.CoinType.Enum, wantCoinTypes) {
		t.Fatalf("coin_type enum=%v want %v", schema.Properties.CoinType.Enum, wantCoinTypes)
	}
	wantChains := []string{"main", "mainnet", "test", "testnet", "regtest"}
	if !reflect.DeepEqual(schema.Properties.Chain.Enum, wantChains) {
		t.Fatalf("chain enum=%v want %v", schema.Properties.Chain.Enum, wantChains)
	}
	pairClauseCount := 0
	if len(schema.AllOf) > 0 {
		pairClauseCount = len(schema.AllOf[0].OneOf)
	}
	if len(schema.AllOf) != 1 || pairClauseCount != 3 {
		t.Fatalf("network pair clauses=%d/%d want 1/3", len(schema.AllOf), pairClauseCount)
	}

	gotPairs := make(map[string]uint32, len(wantChains))
	for i, alternative := range schema.AllOf[0].OneOf {
		coinType := alternative.Properties.CoinType.Const
		if coinType == nil {
			t.Fatalf("network pair %d missing coin_type const", i)
		}
		chains := append([]string(nil), alternative.Properties.Chain.Enum...)
		if alternative.Properties.Chain.Const != nil {
			chains = append(chains, *alternative.Properties.Chain.Const)
		}
		if len(chains) == 0 {
			t.Fatalf("network pair %d missing chain constraint", i)
		}
		for _, chain := range chains {
			if _, exists := gotPairs[chain]; exists {
				t.Fatalf("chain %q appears in more than one network pair", chain)
			}
			gotPairs[chain] = *coinType
		}
	}
	wantPairs := map[string]uint32{
		"main":    mainnetCoinType,
		"mainnet": mainnetCoinType,
		"test":    testnetCoinType,
		"testnet": testnetCoinType,
		"regtest": regtestCoinType,
	}
	if !reflect.DeepEqual(gotPairs, wantPairs) {
		t.Fatalf("network pairs=%v want %v", gotPairs, wantPairs)
	}
}

func TestPlannersRejectCoinTypeMismatchBeforeChainDataReads(t *testing.T) {
	tests := []struct {
		name string
		plan func(string) error
	}{
		{
			name: "send and send-many",
			plan: func(rpcURL string) error {
				_, err := Plan(context.Background(), PlanConfig{
					RPCURL:        rpcURL,
					WalletID:      "hot",
					CoinType:      mainnetCoinType,
					Kind:          types.TxPlanKindWithdrawal,
					Outputs:       []types.TxOutput{{ToAddress: "destination", AmountZat: "1"}},
					ChangeAddress: "change",
				})
				return err
			},
		},
		{
			name: "sweep",
			plan: func(rpcURL string) error {
				_, err := PlanSweep(context.Background(), SweepConfig{
					RPCURL:    rpcURL,
					WalletID:  "hot",
					CoinType:  mainnetCoinType,
					ToAddress: "destination",
				})
				return err
			},
		},
		{
			name: "consolidate",
			plan: func(rpcURL string) error {
				_, err := PlanConsolidate(context.Background(), ConsolidateConfig{
					RPCURL:    rpcURL,
					WalletID:  "hot",
					CoinType:  mainnetCoinType,
					ToAddress: "destination",
					MaxSpends: 2,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var methods []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
				methods = append(methods, request.Method)
				if request.Method != "getblockchaininfo" {
					t.Errorf("unexpected RPC method %q", request.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						"chain":  "regtest",
						"blocks": 100,
						"consensus": map[string]any{
							"nextblock": "c8e71055",
						},
					},
					"error": nil,
					"id":    request.ID,
				})
			}))
			defer srv.Close()

			err := tt.plan(srv.URL)
			assertCodedError(t, err, types.ErrCodeInvalidRequest, "does not match chain")
			if len(methods) != 1 || methods[0] != "getblockchaininfo" {
				t.Fatalf("RPC methods=%v want [getblockchaininfo]", methods)
			}
		})
	}
}
