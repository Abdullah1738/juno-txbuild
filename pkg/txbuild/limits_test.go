package txbuild

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/types"
	"github.com/Abdullah1738/juno-txbuild/internal/logic"
)

func TestPlannerDefaultsAndOverrides(t *testing.T) {
	if got := normalizedMinConfirmations(0); got != DefaultMinConfirmations {
		t.Fatalf("min confirmations default=%d want %d", got, DefaultMinConfirmations)
	}
	if got := normalizedMinConfirmations(7); got != 7 {
		t.Fatalf("min confirmations override=%d want 7", got)
	}
	if got := normalizedFeeMultiplier(0); got != DefaultFeeMultiplier {
		t.Fatalf("fee multiplier default=%d want %d", got, DefaultFeeMultiplier)
	}
	if got := normalizedFeeMultiplier(3); got != 3 {
		t.Fatalf("fee multiplier override=%d want 3", got)
	}
	if err := validateAccount((1 << 31) - 1); err != nil {
		t.Fatalf("valid account rejected: %v", err)
	}
	assertCodedError(t, validateAccount(1<<31), types.ErrCodeInvalidRequest, "below 2147483648")
}

func TestSelectNotesForPlanRequiresConsolidationAboveSignerLimit(t *testing.T) {
	notes := makeUnspentNotes(MaxOrchardSpendNotes+1, 10_000)

	selected, _, err := selectNotesForPlan(notes, 1_000_001, 1, logic.FeePolicy{})
	if err == nil {
		t.Fatal("expected too_many_inputs error")
	}
	if selected != nil {
		t.Fatalf("selected=%d want 0", len(selected))
	}
	assertCodedError(t, err, ErrCodeTooManyInputs, "consolidate notes and retry")
}

func TestSelectNotesForPlanDoesNotMisclassifyArithmeticFailure(t *testing.T) {
	_, _, err := selectNotesForPlan(makeUnspentNotes(1, 10_000), 1, 1, logic.FeePolicy{Multiplier: ^uint64(0)})
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("error=%v want arithmetic overflow", err)
	}
	var coded types.CodedError
	if errors.As(err, &coded) && coded.Code == types.ErrCodeInsufficientBalance {
		t.Fatalf("arithmetic failure was misclassified as insufficient balance: %v", err)
	}
}

func TestSignerCompatiblePlanEnforcesInputAndOutputLimits(t *testing.T) {
	withinLimit := types.TxPlan{
		Notes:   makePlanNotes(MaxOrchardSpendNotes),
		Outputs: make([]types.TxOutput, MaxOrchardOutputs),
	}
	if _, err := signerCompatiblePlan(withinLimit, false); err != nil {
		t.Fatalf("boundary plan without change rejected: %v", err)
	}

	withChange := withinLimit
	_, err := signerCompatiblePlan(withChange, true)
	assertCodedError(t, err, types.ErrCodeInvalidRequest, "201 Orchard outputs including change")

	withChange.Outputs = make([]types.TxOutput, MaxOrchardOutputs-1)
	if _, err := signerCompatiblePlan(withChange, true); err != nil {
		t.Fatalf("boundary plan with change rejected: %v", err)
	}

	tooManyInputs := withinLimit
	tooManyInputs.Notes = makePlanNotes(MaxOrchardSpendNotes + 1)
	_, err = signerCompatiblePlan(tooManyInputs, false)
	assertCodedError(t, err, ErrCodeTooManyInputs, "maximum is 200")

	tooManyOutputs := withinLimit
	tooManyOutputs.Outputs = make([]types.TxOutput, MaxOrchardOutputs+1)
	_, err = signerCompatiblePlan(tooManyOutputs, false)
	assertCodedError(t, err, types.ErrCodeInvalidRequest, "maximum is 200")
}

func TestOrchardChangeRequired(t *testing.T) {
	tests := []struct {
		name     string
		totalIn  uint64
		totalOut uint64
		feeZat   uint64
		want     bool
		wantErr  bool
	}{
		{name: "exact", totalIn: 110, totalOut: 100, feeZat: 10},
		{name: "change", totalIn: 111, totalOut: 100, feeZat: 10, want: true},
		{name: "output exceeds input", totalIn: 99, totalOut: 100, feeZat: 0, wantErr: true},
		{name: "fee exceeds remainder", totalIn: 105, totalOut: 100, feeZat: 6, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := orchardChangeRequired(tt.totalIn, tt.totalOut, tt.feeZat)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("hasChange=%v want %v", got, tt.want)
			}
		})
	}
}

func TestPlanRejectsTooManyOutputsBeforeRPC(t *testing.T) {
	outputs := make([]types.TxOutput, MaxOrchardOutputs+1)
	for i := range outputs {
		outputs[i] = types.TxOutput{ToAddress: "j1example", AmountZat: "1"}
	}

	_, err := Plan(context.Background(), PlanConfig{
		RPCURL:        "http://unused.invalid",
		WalletID:      "hot",
		Kind:          types.TxPlanKindWithdrawal,
		Outputs:       outputs,
		ChangeAddress: "j1change",
	})
	assertCodedError(t, err, types.ErrCodeInvalidRequest, "at most 200")
}

func TestConsolidationMaxSpendsRange(t *testing.T) {
	for _, maxSpends := range []int{-1, 1, MaxOrchardSpendNotes + 1} {
		t.Run(fmt.Sprintf("max_spends_%d", maxSpends), func(t *testing.T) {
			_, err := PlanConsolidate(context.Background(), ConsolidateConfig{
				RPCURL:    "http://unused.invalid",
				WalletID:  "hot",
				ToAddress: "j1destination",
				MaxSpends: maxSpends,
			})
			assertCodedError(t, err, types.ErrCodeInvalidRequest, "between 2 and 200")
		})
	}

	notes := makeUnspentNotes(MaxOrchardSpendNotes+1, 10_000)
	selected, _, err := selectNotesForConsolidation(notes, MaxOrchardSpendNotes, logic.FeePolicy{})
	if err != nil {
		t.Fatalf("select boundary consolidation notes: %v", err)
	}
	if len(selected) != MaxOrchardSpendNotes {
		t.Fatalf("selected=%d want %d", len(selected), MaxOrchardSpendNotes)
	}
}

func makeUnspentNotes(count int, valueZat uint64) []logic.UnspentNote {
	notes := make([]logic.UnspentNote, count)
	for i := range notes {
		notes[i] = logic.UnspentNote{
			TxID:        fmt.Sprintf("%064x", i+1),
			ActionIndex: uint32(i),
			ValueZat:    valueZat,
		}
	}
	return notes
}

func assertCodedError(t *testing.T, err error, code types.ErrorCode, messagePart string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s error", code)
	}
	var coded types.CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error type=%T want types.CodedError: %v", err, err)
	}
	if coded.Code != code {
		t.Fatalf("error code=%q want %q", coded.Code, code)
	}
	if !strings.Contains(coded.Message, messagePart) {
		t.Fatalf("error message=%q want substring %q", coded.Message, messagePart)
	}
}
