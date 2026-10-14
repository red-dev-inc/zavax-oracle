package vm

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// warpQuorumNumerator is the minimum BLS signature weight required (67 of 100).
const warpQuorumNumerator = 67

// decodeWarpMessage parses a hex-encoded warp message, verifies its BLS
// aggregate signature against the destination subnet's validator set, and
// returns the decoded InboundPayload (Zcash height + sender wallet).
//
// The WarpMessenger precompile prepends a 34-byte codec header before the user
// payload. Once stripped, the remainder is an ABI-encoded HeightRequest:
//
//	struct HeightRequest { uint256 height; address sender; bytes32 validationId; }
//
// ICM "self-signing": messages from C-Chain to this subnet are signed by this
// subnet's own validators, so the warp validator set is looked up by
// vm.ctx.SubnetID.
func (vm *VM) decodeWarpMessage(ctx context.Context, warpHex string) (uint64, [20]byte, ids.ID, error) {
	var noWallet [20]byte

	warpHex = strings.TrimPrefix(warpHex, "0x")
	b, err := hex.DecodeString(warpHex)
	if err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("invalid warpMessage hex: %w", err)
	}
	if len(b) == 0 {
		return 0, noWallet, ids.Empty, fmt.Errorf("warpMessage is empty")
	}

	// Parse the signed warp message (UnsignedMessage + BLS aggregate sig).
	warpMsg, err := warp.ParseMessage(b)
	if err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("parse warp message: %w", err)
	}

	vm.log.Info("decodeWarpMessage: parsed warp message",
		zap.String("sourceChainID", warpMsg.UnsignedMessage.SourceChainID.String()),
		zap.Int("payloadLen", len(warpMsg.UnsignedMessage.Payload)),
	)

	// Verify BLS aggregate signature using this subnet's validator set.
	pChainHeight, err := vm.ctx.ValidatorState.GetMinimumHeight(ctx)
	if err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("get P-Chain minimum height: %w", err)
	}

	warpValidatorSets, err := vm.ctx.ValidatorState.GetWarpValidatorSets(ctx, pChainHeight)
	if err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("get warp validator sets: %w", err)
	}
	warpValidators, ok := warpValidatorSets[vm.ctx.SubnetID]
	if !ok {
		return 0, noWallet, ids.Empty, fmt.Errorf("no warp validator set for subnet %s at P-Chain height %d",
			vm.ctx.SubnetID, pChainHeight)
	}

	vm.log.Info("decodeWarpMessage: verifying BLS signature",
		zap.Uint64("pChainHeight", pChainHeight),
		zap.Stringer("signingSubnetID", vm.ctx.SubnetID),
		zap.Uint64("quorumNumerator", warpQuorumNumerator),
	)

	if err := warpMsg.Signature.Verify(
		&warpMsg.UnsignedMessage,
		vm.ctx.NetworkID,
		warpValidators,
		warpQuorumNumerator,
		100,
	); err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("BLS signature invalid: %w", err)
	}

	// The WarpMessenger precompile codec-wraps the user payload in an
	// AddressedCall envelope before storing it in UnsignedMessage.Payload.
	// ParseAddressedCall unwraps it; on failure we fall back to the raw bytes.
	rawPayload := warpMsg.UnsignedMessage.Payload
	abiData := rawPayload
	if ac, err := payload.ParseAddressedCall(rawPayload); err == nil {
		abiData = ac.Payload
	}

	p, err := DecodeInboundPayload(abiData)
	if err != nil {
		return 0, noWallet, ids.Empty, fmt.Errorf("decode inbound payload: %w", err)
	}

	vm.log.Info("decodeWarpMessage: decoded inbound payload",
		zap.Uint64("height", p.Height),
		zap.String("wallet", hex.EncodeToString(p.Wallet[:])),
		zap.Stringer("validationId", p.ValidationID),
	)
	return p.Height, p.Wallet, p.ValidationID, nil
}
