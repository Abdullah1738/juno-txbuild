package txbuild

import (
	"fmt"
	"strings"

	"github.com/Abdullah1738/juno-sdk-go/types"
)

const (
	mainnetCoinType uint32 = 8133
	testnetCoinType uint32 = 8134
	regtestCoinType uint32 = 8135
)

func resolveCoinType(chain string, configured uint32) (uint32, error) {
	network := strings.ToLower(strings.TrimSpace(chain))
	var expected uint32
	switch network {
	case "main":
		expected = mainnetCoinType
	case "test":
		expected = testnetCoinType
	case "regtest":
		expected = regtestCoinType
	default:
		return 0, types.CodedError{Code: types.ErrCodeInvalidRequest, Message: fmt.Sprintf("unknown chain %q", chain)}
	}

	if configured == 0 {
		return expected, nil
	}
	if configured != expected {
		return 0, types.CodedError{
			Code: types.ErrCodeInvalidRequest,
			Message: fmt.Sprintf(
				"coin_type %d does not match chain %q; expected %d",
				configured,
				network,
				expected,
			),
		}
	}
	return configured, nil
}
