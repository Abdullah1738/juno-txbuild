# juno-txbuild

Online `TxPlan` (v0) builder for offline signing.

`juno-txbuild` talks to `junocashd` over RPC to gather chain state, select spend candidates, and produce a `TxPlan` JSON package for offline signing with `juno-txsign`.

## API stability

- The `TxPlan` file format is versioned via `txplan.version` (currently `"v0"`). Breaking changes must be introduced as a new version value.
- For automation/integrations, treat JSON as the stable API surface (`--out` or `--json`). Human-oriented output may change.
- Schemas:
  - `api/txplan.v0.schema.json`
  - `api/txoutputs.schema.json` (for `--outputs-file`)

## CLI

Environment variables (optional; avoid passing secrets on the command line):

- `JUNO_RPC_URL`
- `JUNO_RPC_USER`
- `JUNO_RPC_PASS`
- `JUNO_SCAN_URL` (optional; use `juno-scan` for notes + witnesses)
- `JUNO_SCAN_BEARER_TOKEN` (optional; bearer token for `juno-scan` HTTP API requests)

- `send`: single-output withdrawal plan
- `send-many`: multi-output withdrawal plan (JSON outputs file)
- `sweep`: sweep all spendable notes into 1 output
- `consolidate`: consolidate 2 to 200 notes into 1 output (`--max-spends` defaults to `50`)
- `rebalance`: multi-output rebalance plan (JSON outputs file)

Run `juno-txbuild --help` (or `juno-txbuild <command> -h`) for the complete flag reference.

All commands default to `--minconf 100`. The signer accepts at most 200 Orchard inputs and 200 total Orchard outputs, including implicit change. A plan may have 200 explicit outputs only when it creates no change. If a withdrawal needs more inputs, txbuild returns `too_many_inputs`; consolidate notes and retry.

`--coin-type 0` infers the ZIP-32 coin type from the node (`8133` mainnet, `8134` testnet, `8135` regtest). An explicit value must match the connected network.

## Fees

The base fee calculation is:

- `base_fee_zat = 5000 * max(2, max(spends, outputs))`
- where `outputs` includes the change output when `change > 0`

The shipped default is `--fee-multiplier 20`, matching the pinned `junocashd` 0.9.12 policy. Therefore the default fee is `base_fee_zat * 20` before `--fee-add-zat`.

To pay a higher fee (e.g. during congestion, or to reduce time-to-mine), use:

- `--fee-multiplier <n>` (multiplies the base fee; default `20`)
- `--fee-add-zat <zat>` (adds an absolute zatoshi amount on top)

To avoid creating very small change notes, use:

- `--min-change-zat <zat>`: if computed change is in `(0, min-change-zat)`, `juno-txbuild` adds it to the fee and omits the change output.

To avoid spending very small notes (dust-like inputs), use:

- `--min-note-zat <zat>`: skips spendable notes with value `< min-note-zat` when selecting inputs.

Note: `junocashd` currently rejects conflicting transactions in the mempool (no replacement/RBF), and Orchard spends cannot be fee-bumped via CPFP. Set the fee you want before broadcasting.

## Transaction expiry

All `TxPlan`s include `expiry_height` (Overwinter `nExpiryHeight`) so transactions that are not mined will eventually become invalid.

`juno-txbuild` computes:

- `expiry_height = (chain_tip_height + 1) + expiry_offset`

where `expiry_offset` is controlled by `--expiry-offset` (default: `40`, min: `4`).

For exchange/custody use, pick an `expiry_offset` that is long enough to tolerate short-lived partitions, but short enough to deterministically release notes if a tx gets stuck.

## Optional `juno-scan` integration

By default, `juno-txbuild` uses `junocashd` RPC to enumerate spendable Orchard notes and build witnesses.

If you provide `--scan-url` (or set `JUNO_SCAN_URL`), `juno-txbuild` will source unspent notes + witness paths from `juno-scan` instead, avoiding a full chain rescan per invocation. In this mode, `--wallet-id` is used as the `wallet_id` for `juno-scan`.

Before reading notes, txbuild requires scanner health status `ok` and an exact scanner/node height and hash match at the captured planning anchor. Witnesses are requested at that explicit height. Before returning a plan, txbuild re-reads every selected note, verifies the same node and scanner anchor, and verifies that the node network, next-block consensus branch, and remaining expiry window are still compatible. A reorg, scanner-tip change, selected-note spend, consensus upgrade, or near-expiry plan during planning therefore fails closed. The node may advance beyond the anchor only while that anchor remains canonical, the next-block branch is unchanged, and the plan remains acceptable to the node's expiring-soon policy.

If `juno-scan` is configured with `-api-bearer-token`, pass `--scan-bearer-token` (or set `JUNO_SCAN_BEARER_TOKEN`) so `juno-txbuild` will include `Authorization: Bearer <token>` on all `juno-scan` requests.

## Concurrency and note reservations

The final selected-note recheck is not a reservation. `juno-txbuild` does not coordinate concurrent planners, and scanner pending-spend state is observational rather than a lock.

Every returned note has a required, unique canonical `note_id` in the form `<64-lowercase-hex-source-txid>:<base-10-uint32-action-index>`. The exchange must serialize planning per wallet and atomically reserve every returned `notes[].note_id` in its own database before signing. Reservation is all-or-nothing: reject a plan with a missing, malformed, duplicate, or already-reserved note ID, then rebuild from a fresh snapshot. Never sign two plans with overlapping note IDs. Keep reservations until the transaction is confirmed or is conclusively rejected or expired and scanner state has reconciled; only then release them.

## File formats

### `TxOutput` (`--outputs-file`)

`send-many` and `rebalance` accept `--outputs-file <path|->` containing a JSON array of `TxOutput` items:

```json
[
  { "to_address": "j*1...", "amount_zat": "100000" },
  { "to_address": "j*1...", "amount_zat": "250000", "memo_hex": "..." }
]
```

See `api/txoutputs.schema.json`.

### `TxPlan` (stdout / `--out`)

All commands produce a `TxPlan` JSON object (pretty-printed to stdout by default). Use `--out <path>` to write the plan to a file (mode `0600`).

The `TxPlan` schema is documented in `api/txplan.v0.schema.json`.

### `--json` envelope

When `--json` is set, output is wrapped:

- success: `{"version":"v1","status":"ok","data":<TxPlan>}`
- error: `{"version":"v1","status":"err","error":{"code":"...","message":"..."}}`

## Errors

Error codes are designed to be machine-readable:

- `invalid_request`
- `insufficient_balance`
- `too_many_inputs`
- `no_liquidity_in_hot`
- `not_found`

## Testing

`make test` runs unit + integration + e2e suites (Dockerized `junocashd` regtest).
