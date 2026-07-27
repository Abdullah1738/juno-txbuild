package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/types"
)

func TestUsageDocumentsSafeDefaultsAndLimits(t *testing.T) {
	var out, errBuf bytes.Buffer

	code := RunWithIO([]string{"--help"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q", code, errBuf.String())
	}
	for _, want := range []string{
		"--max-spends <2..200>",
		"--exclude-note-id <txid:index>",
		"Defaults: --minconf 100, --fee-multiplier 20",
		"signer limit: 200 inputs and 200 total outputs including change",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("usage missing %q:\n%s", want, out.String())
		}
	}
}

func TestSendRejectsMalformedAndDuplicateExcludedNoteIDsBeforeRPC(t *testing.T) {
	canonical := strings.Repeat("a", 64) + ":0"
	tests := []struct {
		name       string
		exclusions []string
		want       string
	}{
		{name: "malformed", exclusions: []string{"not-a-note-id"}, want: "must match"},
		{name: "duplicate", exclusions: []string{canonical, canonical}, want: "duplicates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{
				"send",
				"--rpc-url", "http://127.0.0.1:1",
				"--wallet-id", "hot",
				"--to", "destination",
				"--amount-zat", "1",
				"--change-address", "change",
				"--json",
			}
			for _, noteID := range tt.exclusions {
				args = append(args, "--exclude-note-id", noteID)
			}

			var out, errBuf bytes.Buffer
			code := RunWithIO(args, &out, &errBuf)
			if code != 1 {
				t.Fatalf("exit code=%d stderr=%q", code, errBuf.String())
			}
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Error.Code != string(types.ErrCodeInvalidRequest) || !strings.Contains(envelope.Error.Message, tt.want) {
				t.Fatalf("unexpected error: %+v", envelope.Error)
			}
		})
	}
}

func TestConsolidateRejectsMaxSpendsOutsideRangeBeforeRPC(t *testing.T) {
	for _, value := range []string{"-1", "0", "1", "201"} {
		t.Run(value, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := RunWithIO([]string{"consolidate", "--max-spends", value, "--json"}, &out, &errBuf)
			if code != 1 {
				t.Fatalf("exit code=%d stderr=%q", code, errBuf.String())
			}
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Error.Code != string(types.ErrCodeInvalidRequest) || !strings.Contains(envelope.Error.Message, "between 2 and 200") {
				t.Fatalf("unexpected error: %+v", envelope.Error)
			}
		})
	}
}

func TestPlannerUint32FlagsRejectOverflowAndHardenedAccount(t *testing.T) {
	maxUint32 := uint64(^uint32(0))
	if _, _, _, err := plannerUint32Flags(maxUint32, (1<<31)-1, maxUint32); err != nil {
		t.Fatalf("valid boundaries rejected: %v", err)
	}
	for name, values := range map[string][3]uint64{
		"coin type overflow": {maxUint32 + 1, 0, 40},
		"hardened account":   {8135, 1 << 31, 40},
		"expiry overflow":    {8135, 0, maxUint32 + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := plannerUint32Flags(values[0], values[1], values[2]); err == nil {
				t.Fatal("invalid values accepted")
			}
		})
	}
}

func TestWriteErr_JSON_IncludesVersion(t *testing.T) {
	var out, errBuf bytes.Buffer

	code := writeErr(&out, &errBuf, true, types.ErrCodeInvalidRequest, "bad request")
	if code != 1 {
		t.Fatalf("unexpected exit code: %d", code)
	}

	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v (%q)", err, out.String())
	}
	if v["version"] != "v1" || v["status"] != "err" {
		t.Fatalf("unexpected json: %v", v)
	}
}

func TestWritePlan_JSON_IncludesVersion(t *testing.T) {
	var out, errBuf bytes.Buffer

	plan := types.TxPlan{
		Version: types.V0,
		Kind:    types.TxPlanKindWithdrawal,
	}

	code := writePlan(&out, &errBuf, true, "", plan)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d (stderr=%q)", code, errBuf.String())
	}

	var v map[string]any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("invalid json: %v (%q)", err, out.String())
	}
	if v["version"] != "v1" || v["status"] != "ok" {
		t.Fatalf("unexpected json: %v", v)
	}
}
