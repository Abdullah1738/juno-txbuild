package txbuild

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/types"
	"github.com/Abdullah1738/juno-txbuild/internal/logic"
)

func TestValidateExcludedNoteIDsRequiresCanonicalUniqueIDs(t *testing.T) {
	txid := strings.Repeat("a", 64)
	otherTxID := strings.Repeat("b", 64)
	tests := []struct {
		name        string
		noteIDs     []string
		wantMessage string
	}{
		{name: "empty list"},
		{name: "canonical boundaries", noteIDs: []string{txid + ":0", otherTxID + ":4294967295"}},
		{name: "missing", noteIDs: []string{""}, wantMessage: "excluded_note_ids[0] required"},
		{name: "malformed txid", noteIDs: []string{"abc:0"}, wantMessage: "must match"},
		{name: "uppercase txid", noteIDs: []string{strings.Repeat("A", 64) + ":0"}, wantMessage: "must match"},
		{name: "surrounding whitespace", noteIDs: []string{" " + txid + ":0"}, wantMessage: "must match"},
		{name: "leading zero action", noteIDs: []string{txid + ":01"}, wantMessage: "must match"},
		{name: "action overflow", noteIDs: []string{txid + ":4294967296"}, wantMessage: "fit uint32"},
		{name: "duplicate", noteIDs: []string{txid + ":0", txid + ":0"}, wantMessage: "duplicates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateExcludedNoteIDs(tt.noteIDs)
			if tt.wantMessage == "" {
				if err != nil {
					t.Fatalf("canonical exclusions rejected: %v", err)
				}
				if len(got) != len(tt.noteIDs) {
					t.Fatalf("validated IDs=%d want %d", len(got), len(tt.noteIDs))
				}
				return
			}
			assertCodedError(t, err, types.ErrCodeInvalidRequest, tt.wantMessage)
		})
	}
}

func TestFilterExcludedUnspentNotesChangesSelectionDeterministically(t *testing.T) {
	notes := []logic.UnspentNote{
		{TxID: strings.Repeat("b", 64), ActionIndex: 2, ValueZat: 1_200_000},
		{TxID: strings.Repeat("a", 64), ActionIndex: 1, ValueZat: 1_100_000},
	}

	selected, _, err := selectNotesForPlan(notes, 1_000_000, 1, logic.FeePolicy{Multiplier: 1})
	if err != nil {
		t.Fatalf("select baseline: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected=%d want 1", len(selected))
	}
	firstID := fmt.Sprintf("%s:%d", selected[0].TxID, selected[0].ActionIndex)

	filtered := filterExcludedUnspentNotes(notes, map[string]struct{}{firstID: {}})
	selected, _, err = selectNotesForPlan(filtered, 1_000_000, 1, logic.FeePolicy{Multiplier: 1})
	if err != nil {
		t.Fatalf("select alternate: %v", err)
	}
	if len(selected) != 1 || fmt.Sprintf("%s:%d", selected[0].TxID, selected[0].ActionIndex) == firstID {
		t.Fatalf("excluded note selected: %+v", selected)
	}
	if !slices.Equal(filtered, []logic.UnspentNote{notes[0]}) {
		t.Fatalf("filter changed candidate order: %+v", filtered)
	}

	allExcluded := map[string]struct{}{
		fmt.Sprintf("%s:%d", notes[0].TxID, notes[0].ActionIndex): {},
		fmt.Sprintf("%s:%d", notes[1].TxID, notes[1].ActionIndex): {},
	}
	_, _, err = selectNotesForPlan(filterExcludedUnspentNotes(notes, allExcluded), 1_000_000, 1, logic.FeePolicy{Multiplier: 1})
	assertCodedError(t, err, types.ErrCodeInsufficientBalance, "insufficient funds")
}

func TestExcludedNoteIDsValidatedBeforeRPCForEveryPlanner(t *testing.T) {
	bad := []string{"not-a-note-id"}
	tests := []struct {
		name string
		plan func() error
	}{
		{
			name: "send",
			plan: func() error {
				_, err := PlanSend(context.Background(), SendConfig{
					RPCURL: "http://127.0.0.1:1", WalletID: "hot", ToAddress: "to", AmountZat: "1", ChangeAddress: "change", ExcludedNoteIDs: bad,
				})
				return err
			},
		},
		{
			name: "send-many and rebalance",
			plan: func() error {
				_, err := Plan(context.Background(), PlanConfig{
					RPCURL: "http://127.0.0.1:1", WalletID: "hot", Kind: types.TxPlanKindWithdrawal, Outputs: []types.TxOutput{{ToAddress: "to", AmountZat: "1"}}, ChangeAddress: "change", ExcludedNoteIDs: bad,
				})
				return err
			},
		},
		{
			name: "sweep",
			plan: func() error {
				_, err := PlanSweep(context.Background(), SweepConfig{
					RPCURL: "http://127.0.0.1:1", WalletID: "hot", ToAddress: "to", ExcludedNoteIDs: bad,
				})
				return err
			},
		},
		{
			name: "consolidate",
			plan: func() error {
				_, err := PlanConsolidate(context.Background(), ConsolidateConfig{
					RPCURL: "http://127.0.0.1:1", WalletID: "hot", ToAddress: "to", MaxSpends: 2, ExcludedNoteIDs: bad,
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan()
			var coded types.CodedError
			if !errors.As(err, &coded) || coded.Code != types.ErrCodeInvalidRequest || !strings.Contains(coded.Message, "excluded_note_ids[0]") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestFilterExcludedSpendableNotesPreservesMetadataAndIgnoresUnknownIDs(t *testing.T) {
	notes := []spendableNote{
		{TxID: strings.Repeat("a", 64), ActionIndex: 0, Height: 10, Position: 11, ValueZat: 12},
		{TxID: strings.Repeat("b", 64), ActionIndex: 1, Height: 20, Position: 21, ValueZat: 22},
	}
	excluded := map[string]struct{}{
		strings.Repeat("f", 64) + ":9": {},
		notes[0].TxID + ":0":           {},
	}
	got := filterExcludedSpendableNotes(notes, excluded)
	if !slices.Equal(got, notes[1:]) {
		t.Fatalf("filtered notes=%+v want %+v", got, notes[1:])
	}
}
