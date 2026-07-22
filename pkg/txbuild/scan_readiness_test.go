package txbuild

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Abdullah1738/juno-sdk-go/junoscan"
)

type blockHashReaderFunc func(context.Context, int64) (string, error)

func (f blockHashReaderFunc) GetBlockHash(ctx context.Context, height int64) (string, error) {
	return f(ctx, height)
}

func TestCaptureScannerAnchor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        string
		scannerHeight any
		scannerHash   any
		wantError     string
	}{
		{name: "match", status: "ok", scannerHeight: int64(100), scannerHash: "node-hash"},
		{name: "lag", status: "ok", scannerHeight: int64(99), scannerHash: "node-hash", wantError: "scanner height mismatch"},
		{name: "hash mismatch", status: "ok", scannerHeight: int64(100), scannerHash: "other-hash", wantError: "scanner hash mismatch"},
		{name: "degraded", status: "degraded", scannerHeight: int64(100), scannerHash: "node-hash", wantError: "scanner health status"},
		{name: "missing height", status: "ok", scannerHash: "node-hash", wantError: "missing scanned_height"},
		{name: "missing hash", status: "ok", scannerHeight: int64(100), wantError: "missing scanned_hash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
				response := map[string]any{"status": tt.status}
				if tt.scannerHeight != nil {
					response["scanned_height"] = tt.scannerHeight
				}
				if tt.scannerHash != nil {
					response["scanned_hash"] = tt.scannerHash
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			sc, err := junoscan.New(srv.URL)
			if err != nil {
				t.Fatalf("junoscan.New: %v", err)
			}
			rpc := blockHashReaderFunc(func(_ context.Context, height int64) (string, error) {
				if height != 100 {
					return "", errors.New("unexpected height")
				}
				return "node-hash", nil
			})

			snapshot, err := captureScannerAnchor(context.Background(), rpc, sc, 100)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("captureScannerAnchor: %v", err)
				}
				if snapshot.height != 100 || snapshot.hash != "node-hash" {
					t.Fatalf("snapshot=%+v", snapshot)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestVerifyScannerAnchorDetectsMidRequestChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		secondNodeHash      string
		secondScannerStatus string
		secondScannerHeight *int64
		secondScannerHash   *string
		wantError           string
	}{
		{name: "stable", secondNodeHash: "anchor-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(100), secondScannerHash: stringPointer("anchor-hash")},
		{name: "node reorg", secondNodeHash: "new-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(100), secondScannerHash: stringPointer("anchor-hash"), wantError: "node anchor hash changed during planning"},
		{name: "coordinated node and scanner reorg", secondNodeHash: "new-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(100), secondScannerHash: stringPointer("new-hash"), wantError: "node anchor hash changed during planning"},
		{name: "scanner height changed", secondNodeHash: "anchor-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(101), secondScannerHash: stringPointer("anchor-hash"), wantError: "scanner height mismatch"},
		{name: "scanner hash changed", secondNodeHash: "anchor-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(100), secondScannerHash: stringPointer("new-hash"), wantError: "scanner hash mismatch"},
		{name: "scanner degraded", secondNodeHash: "anchor-hash", secondScannerStatus: "degraded", secondScannerHeight: int64Pointer(100), secondScannerHash: stringPointer("anchor-hash"), wantError: "scanner health status"},
		{name: "scanner missing height", secondNodeHash: "anchor-hash", secondScannerStatus: "ok", secondScannerHash: stringPointer("anchor-hash"), wantError: "missing scanned_height"},
		{name: "scanner missing hash", secondNodeHash: "anchor-hash", secondScannerStatus: "ok", secondScannerHeight: int64Pointer(100), wantError: "missing scanned_hash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var healthCalls atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
				response := map[string]any{
					"status":         "ok",
					"scanned_height": int64(100),
					"scanned_hash":   "anchor-hash",
				}
				if healthCalls.Add(1) > 1 {
					response = map[string]any{"status": tt.secondScannerStatus}
					if tt.secondScannerHeight != nil {
						response["scanned_height"] = *tt.secondScannerHeight
					}
					if tt.secondScannerHash != nil {
						response["scanned_hash"] = *tt.secondScannerHash
					}
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			sc, err := junoscan.New(srv.URL)
			if err != nil {
				t.Fatalf("junoscan.New: %v", err)
			}
			var nodeCalls atomic.Int32
			rpc := blockHashReaderFunc(func(_ context.Context, height int64) (string, error) {
				if height != 100 {
					return "", errors.New("unexpected height")
				}
				if nodeCalls.Add(1) == 1 {
					return "anchor-hash", nil
				}
				return tt.secondNodeHash, nil
			})

			snapshot, err := captureScannerAnchor(context.Background(), rpc, sc, 100)
			if err != nil {
				t.Fatalf("captureScannerAnchor: %v", err)
			}
			err = verifyScannerAnchor(context.Background(), rpc, sc, snapshot)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("verifyScannerAnchor: %v", err)
				}
				if nodeCalls.Load() != 2 || healthCalls.Load() != 2 {
					t.Fatalf("calls node=%d health=%d want 2 each", nodeCalls.Load(), healthCalls.Load())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestVerifyNodeAnchorDetectsDirectPlannerReorg(t *testing.T) {
	t.Parallel()

	snapshot := scannerAnchorSnapshot{height: 100, hash: "anchor-hash"}
	for _, tt := range []struct {
		name      string
		hash      string
		wantError string
	}{
		{name: "stable", hash: "anchor-hash"},
		{name: "reorg", hash: "replacement-hash", wantError: "anchor hash changed"},
		{name: "empty hash", hash: " ", wantError: "block hash is empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifyNodeAnchor(context.Background(), blockHashReaderFunc(func(_ context.Context, height int64) (string, error) {
				if height != snapshot.height {
					return "", errors.New("unexpected height")
				}
				return tt.hash, nil
			}), snapshot)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("verifyNodeAnchor: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error=%v want substring %q", err, tt.wantError)
			}
		})
	}
}

func int64Pointer(value int64) *int64 { return &value }

func stringPointer(value string) *string { return &value }

func TestOrchardWitnessAtAnchorSendsAndChecksExplicitHeight(t *testing.T) {
	t.Parallel()

	const anchorHeight = int64(321)
	var requested *int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/orchard/witness", func(w http.ResponseWriter, r *http.Request) {
		var req junoscan.WitnessRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		requested = req.AnchorHeight
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"anchor_height": anchorHeight,
			"root":          "root",
			"paths": []map[string]any{
				{"position": 7, "auth_path": []string{"path"}},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	sc, err := junoscan.New(srv.URL)
	if err != nil {
		t.Fatalf("junoscan.New: %v", err)
	}
	if _, err := orchardWitnessAtAnchor(context.Background(), sc, anchorHeight, []uint32{7}); err != nil {
		t.Fatalf("orchardWitnessAtAnchor: %v", err)
	}
	if requested == nil || *requested != anchorHeight {
		t.Fatalf("requested anchor=%v want %d", requested, anchorHeight)
	}
}

func TestOrchardWitnessAtAnchorRejectsReturnedHeightMismatch(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/orchard/witness", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"anchor_height": 320,
			"root":          "root",
			"paths": []map[string]any{
				{"position": 7, "auth_path": []string{"path"}},
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	sc, err := junoscan.New(srv.URL)
	if err != nil {
		t.Fatalf("junoscan.New: %v", err)
	}
	_, err = orchardWitnessAtAnchor(context.Background(), sc, 321, []uint32{7})
	if err == nil || !strings.Contains(err.Error(), "anchor_height mismatch") {
		t.Fatalf("error=%v want anchor_height mismatch", err)
	}
}
