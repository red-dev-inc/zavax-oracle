package vm

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// uptimeVerifier decides whether this node is willing to put its BLS signature
// on a Warp message. It backs the ACP-118 signature handler, which is what a
// signature aggregator (the Builder Console, icm-services, avalanche-cli) talks
// to when it collects the quorum a StakingManager needs.
//
// Two kinds of message are signable:
//
//  1. A ValidationUptimeMessage, but ONLY if this node independently measured
//     at least the uptime being claimed. This check is the entire security
//     property of PoS rewards on this L1: without it anyone could ask the
//     validators to attest to a fabricated uptime and mint rewards that were
//     never earned.
//
//  2. An outbound oracle message this node itself emitted in Accept(), looked
//     up by exact message ID in the outbox. Those are derived from accepted
//     blocks, so membership in the outbox is itself the proof of validity.
//
// Anything else is refused. Refusing is always safe: a signature that is never
// produced simply means the aggregator falls short of quorum.
type uptimeVerifier struct {
	vm *VM
}

// Verify implements acp118.Verifier.
func (v *uptimeVerifier) Verify(_ context.Context, msg *warp.UnsignedMessage, _ []byte) *common.AppError {
	if err := v.vm.checkSignable(msg); err != nil {
		v.vm.log.Debug("uptime: refusing to sign warp message",
			zap.Stringer("messageID", msg.ID()),
			zap.Error(err),
		)
		return &common.AppError{
			Code:    p2p.ErrUnexpected.Code,
			Message: err.Error(),
		}
	}
	return nil
}

var _ acp118.Verifier = (*uptimeVerifier)(nil)

// checkSignable reports whether this node may sign msg, returning a
// descriptive error if not. It is shared by the ACP-118 p2p handler and the
// /warp HTTP signing endpoint so both enforce identical rules.
func (vm *VM) checkSignable(msg *warp.UnsignedMessage) error {
	// A node may only ever sign messages attributed to its own chain. The Warp
	// signer enforces this too; checking here makes the rule explicit and
	// produces a clearer error than a signing failure.
	if msg.SourceChainID != vm.ctx.ChainID {
		return fmt.Errorf("sourceChainID mismatch: got %s, want %s", msg.SourceChainID, vm.ctx.ChainID)
	}
	if msg.NetworkID != vm.ctx.NetworkID {
		return fmt.Errorf("networkID mismatch: got %d, want %d", msg.NetworkID, vm.ctx.NetworkID)
	}

	uptimeMsg, err := parseUptimeAddressedCall(msg.Payload)
	switch {
	case err == nil:
		return vm.checkUptimeClaim(uptimeMsg)

	case errors.Is(err, errNotUptimeMessage):
		// Not an uptime proof. Fall through to the outbox check below.

	default:
		// It is an uptime proof, but a malformed one.
		return err
	}

	if vm.hasOutboundWarpMsg(msg.ID()) {
		return nil
	}
	return errors.New("message is neither a valid uptime proof nor a known outbound oracle message")
}

// checkUptimeClaim refuses any claim larger than the uptime this node measured
// for itself. Equal or smaller claims are fine: a caller is always free to
// under-claim.
func (vm *VM) checkUptimeClaim(msg uptimeMessage) error {
	measured, info, err := vm.uptimeTracker.uptimeSeconds(msg.ValidationID)
	if err != nil {
		return fmt.Errorf("no uptime for validationID %s: %w", msg.ValidationID, err)
	}

	if msg.UptimeSeconds > measured {
		return fmt.Errorf(
			"claimed uptime %ds exceeds measured uptime %ds for %s",
			msg.UptimeSeconds, measured, info.nodeID,
		)
	}

	vm.log.Info("uptime: signing uptime proof",
		zap.Stringer("validationID", msg.ValidationID),
		zap.Stringer("nodeID", info.nodeID),
		zap.Uint64("claimedSeconds", msg.UptimeSeconds),
		zap.Uint64("measuredSeconds", measured),
	)
	return nil
}

// checkNotForgedUptime is the narrower guard used by the pre-existing /warp
// HTTP endpoint. That endpoint signs any message attributed to this chain, a
// behaviour the ICM relayer depends on, so it is left intact for oracle
// messages — but an uptime proof must never be obtainable that way, since it
// would sidestep the measurement check entirely.
func (vm *VM) checkNotForgedUptime(msg *warp.UnsignedMessage) error {
	uptimeMsg, err := parseUptimeAddressedCall(msg.Payload)
	if errors.Is(err, errNotUptimeMessage) {
		return nil // not an uptime proof; the endpoint's existing rules apply
	}
	if err != nil {
		return err
	}
	return vm.checkUptimeClaim(uptimeMsg)
}

// hasOutboundWarpMsg reports whether msgID is a message this node emitted.
func (vm *VM) hasOutboundWarpMsg(msgID ids.ID) bool {
	vm.outboundWarpMsgsMu.RLock()
	defer vm.outboundWarpMsgsMu.RUnlock()
	_, ok := vm.outboundWarpMsgs[msgID]
	return ok
}
