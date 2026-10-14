package vm

import (
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// abiSlotSize is the byte width of a single ABI-encoded value slot.
const abiSlotSize = 32

// abiSlot returns the nth 32-byte ABI slot from data (zero-indexed).
func abiSlot(data []byte, n int) []byte { return data[n*abiSlotSize : (n+1)*abiSlotSize] }

// abiEncodeUint256 right-aligns v in a 32-byte ABI uint256 slot.
func abiEncodeUint256(v uint64) []byte {
	slot := make([]byte, abiSlotSize)
	binary.BigEndian.PutUint64(slot[24:], v)
	return slot
}

// abiDecodeUint256 reads a uint64 from the right-aligned position in a 32-byte ABI slot.
func abiDecodeUint256(slot []byte) uint64 { return binary.BigEndian.Uint64(slot[24:]) }

// abiEncodeAddress right-aligns a 20-byte address in a 32-byte ABI address slot.
func abiEncodeAddress(addr [20]byte) []byte {
	slot := make([]byte, abiSlotSize)
	copy(slot[12:], addr[:])
	return slot
}

// abiDecodeAddress reads a 20-byte address from the right-aligned position in a 32-byte ABI slot.
func abiDecodeAddress(slot []byte) (addr [20]byte) {
	copy(addr[:], slot[12:])
	return
}

// InboundPayload is the decoded form of the warp message emitted by
// ZcashHeightGateway.sol on C-Chain:
//
//	struct HeightRequest { uint256 height; address sender; bytes32 validationId; }
//	abi.encode(HeightRequest({height: height, sender: msg.sender, validationId: validationId}))
//
// ValidationID is the zero value when the caller had no validator preference;
// the fee for that request went to the gateway's default rbrAssociation
// wallet. Otherwise it is the ACP-99 validationID (same identifier used by
// platform.getCurrentValidators and ValidatorRewardAccrual.sol) whose
// StakingManager-registered reward wallet was paid. Payout already happened
// on C-Chain by the time the VM sees this message, so ValidationID is
// informational here (logging/attribution) rather than something the VM
// must act on.
type InboundPayload struct {
	Height       uint64
	Wallet       [20]byte // sender address from ZcashHeightGateway
	ValidationID ids.ID
}

// EncodeInboundPayload encodes p as abi.encode(uint256 height, address sender, bytes32 validationId).
// The output matches exactly what ZcashHeightGateway.sol emits on C-Chain.
func EncodeInboundPayload(p InboundPayload) []byte {
	out := make([]byte, 3*abiSlotSize)
	copy(abiSlot(out, 0), abiEncodeUint256(p.Height))
	copy(abiSlot(out, 1), abiEncodeAddress(p.Wallet))
	copy(abiSlot(out, 2), p.ValidationID[:])
	return out
}

// DecodeInboundPayload parses an ABI-encoded HeightRequest payload.
// data must be the raw user payload after the 34-byte WarpMessenger header is stripped.
func DecodeInboundPayload(data []byte) (InboundPayload, error) {
	if len(data) < 3*abiSlotSize {
		return InboundPayload{}, fmt.Errorf(
			"inbound payload: need %d bytes for HeightRequest, got %d", 3*abiSlotSize, len(data))
	}
	p := InboundPayload{
		Height: abiDecodeUint256(abiSlot(data, 0)),
		Wallet: abiDecodeAddress(abiSlot(data, 1)),
	}
	copy(p.ValidationID[:], abiSlot(data, 2))
	if p.Height == 0 {
		return InboundPayload{}, fmt.Errorf("inbound payload: height 0 is reserved")
	}
	return p, nil
}

// OutboundPayload is the content of the warp message sent from the VM back to
// C-Chain to trigger the fee transfer in VMWarpReceiver.sol:
//
//	struct Payload { address wallet; uint256 zcashHeight; }
//	abi.decode(payload, (Payload))
type OutboundPayload struct {
	Wallet      [20]byte
	ZcashHeight uint64
}

// Encode serialises the payload as abi.encode(address wallet, uint256 zcashHeight).
// VMWarpReceiver.sol decodes this with abi.decode(payload, (Payload)).
func (p OutboundPayload) Encode() []byte {
	out := make([]byte, 2*abiSlotSize)
	copy(abiSlot(out, 0), abiEncodeAddress(p.Wallet))
	copy(abiSlot(out, 1), abiEncodeUint256(p.ZcashHeight))
	return out
}

// DecodeOutboundPayload parses an ABI-encoded Payload.
// Used by tests to verify round-trip consistency.
func DecodeOutboundPayload(data []byte) (OutboundPayload, error) {
	if len(data) < 2*abiSlotSize {
		return OutboundPayload{}, fmt.Errorf(
			"outbound payload: need %d bytes for Payload, got %d", 2*abiSlotSize, len(data))
	}
	return OutboundPayload{
		Wallet:      abiDecodeAddress(abiSlot(data, 0)),
		ZcashHeight: abiDecodeUint256(abiSlot(data, 1)),
	}, nil
}
