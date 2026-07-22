package txbuild

import (
	"errors"
	"fmt"

	"github.com/Abdullah1738/juno-sdk-go/types"
	"github.com/Abdullah1738/juno-txbuild/internal/logic"
)

const (
	// MaxOrchardSpendNotes is the maximum number of Orchard notes accepted by
	// juno-txsign for one transaction.
	MaxOrchardSpendNotes = 200
	// MaxOrchardOutputs is the maximum number of Orchard outputs accepted by
	// juno-txsign for one transaction.
	MaxOrchardOutputs = 200

	DefaultMinConfirmations int64  = 100
	DefaultFeeMultiplier    uint64 = 20
	// txExpiringSoonThreshold mirrors junocashd's mempool admission policy.
	txExpiringSoonThreshold uint64 = 3

	ErrCodeTooManyInputs types.ErrorCode = "too_many_inputs"
)

func normalizedMinConfirmations(value int64) int64 {
	if value <= 0 {
		return DefaultMinConfirmations
	}
	return value
}

func normalizedFeeMultiplier(value uint64) uint64 {
	if value == 0 {
		return DefaultFeeMultiplier
	}
	return value
}

func validateAccount(account uint32) error {
	if account >= 1<<31 {
		return types.CodedError{Code: types.ErrCodeInvalidRequest, Message: "account must be below 2147483648"}
	}
	return nil
}

func ensureOrchardSpendLimit(count int) error {
	if count <= MaxOrchardSpendNotes {
		return nil
	}
	return types.CodedError{
		Code: ErrCodeTooManyInputs,
		Message: fmt.Sprintf(
			"transaction requires %d Orchard inputs; maximum is %d; consolidate notes and retry",
			count,
			MaxOrchardSpendNotes,
		),
	}
}

func signerCompatiblePlan(plan types.TxPlan, hasChange bool) (types.TxPlan, error) {
	if err := ensureOrchardSpendLimit(len(plan.Notes)); err != nil {
		return types.TxPlan{}, err
	}
	outputCount := len(plan.Outputs)
	if hasChange {
		outputCount++
	}
	if outputCount > MaxOrchardOutputs {
		return types.TxPlan{}, types.CodedError{
			Code:    types.ErrCodeInvalidRequest,
			Message: fmt.Sprintf("transaction has %d Orchard outputs including change; maximum is %d", outputCount, MaxOrchardOutputs),
		}
	}
	return plan, nil
}

func orchardChangeRequired(totalIn, totalOut, feeZat uint64) (bool, error) {
	if totalIn < totalOut {
		return false, errors.New("txbuild: invalid transaction totals")
	}
	remaining := totalIn - totalOut
	if remaining < feeZat {
		return false, errors.New("txbuild: invalid transaction totals")
	}
	return remaining > feeZat, nil
}

func selectNotesForPlan(notes []logic.UnspentNote, amountZat uint64, outputCount int, feePolicy logic.FeePolicy) ([]logic.UnspentNote, uint64, error) {
	selected, feeZat, err := logic.SelectNotesWithFeePolicy(notes, amountZat, outputCount, feePolicy)
	if err != nil {
		if errors.Is(err, logic.ErrInsufficientFunds) {
			return nil, 0, types.CodedError{Code: types.ErrCodeInsufficientBalance, Message: "insufficient funds"}
		}
		return nil, 0, fmt.Errorf("txbuild: select notes: %w", err)
	}
	if err := ensureOrchardSpendLimit(len(selected)); err != nil {
		return nil, 0, err
	}
	return selected, feeZat, nil
}
