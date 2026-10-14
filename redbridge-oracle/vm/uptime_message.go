package vm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

// This file implements the `ValidationUptimeMessage` wire format consumed by
// the StakingManager contract to compute staking rewards.
//
// The contract requires an exact 46-byte payload (see ValidatorMessages.sol,
// `unpackValidationUptimeMessage`):
//
//	Offset  Size  Field
//	0       2     codecID     = 0x0000
//	2       4     typeID      = 0x00000000
//	6       32    validationID
//	38      8     uptime (seconds, big-endian)
//
// Those bytes are then wrapped in a Warp AddressedCall whose SourceAddress is
// EMPTY. The empty source address is what proves to the contract that a
// validator node signed the proof directly, rather than it having been
// produced by an arbitrary on-chain message. StakingManager rejects any uptime
// proof with a non-zero originSenderAddress.
//
// This format is defined by Subnet-EVM (warp/messages/validator_uptime.go),
// where ValidatorUptime is the first and only type registered in a linear
// codec at version 0 — hence codecID 0, typeID 0. We hand-pack the bytes
// rather than importing that codec so this VM keeps a single, readable
// definition of the format it signs.

const (
	// uptimeMessageCodecID is the codec version Subnet-EVM registers the
	// ValidatorUptime type under, and the only value StakingManager accepts.
	uptimeMessageCodecID uint16 = 0

	// uptimeMessageTypeID is ValidatorUptime's type ID within that codec.
	uptimeMessageTypeID uint32 = 0

	// uptimeMessageLen is the exact payload length StakingManager requires.
	uptimeMessageLen = 2 + 4 + ids.IDLen + 8 // 46
)

// errNotUptimeMessage signals that a payload is well-formed but simply is not
// a ValidationUptimeMessage. Callers use this to distinguish "not our message"
// from "malformed / hostile message".
var errNotUptimeMessage = errors.New("not a validation uptime message")

// uptimeMessage is the decoded form of a ValidationUptimeMessage.
type uptimeMessage struct {
	ValidationID  ids.ID
	UptimeSeconds uint64
}

// marshal serialises the message into the 46-byte form StakingManager expects.
func (m uptimeMessage) marshal() []byte {
	buf := make([]byte, uptimeMessageLen)
	binary.BigEndian.PutUint16(buf[0:2], uptimeMessageCodecID)
	binary.BigEndian.PutUint32(buf[2:6], uptimeMessageTypeID)
	copy(buf[6:6+ids.IDLen], m.ValidationID[:])
	binary.BigEndian.PutUint64(buf[6+ids.IDLen:], m.UptimeSeconds)
	return buf
}

// parseUptimeMessage decodes the 46-byte ValidationUptimeMessage payload.
// It returns errNotUptimeMessage if the bytes are not this message type.
func parseUptimeMessage(b []byte) (uptimeMessage, error) {
	if len(b) != uptimeMessageLen {
		return uptimeMessage{}, errNotUptimeMessage
	}
	if binary.BigEndian.Uint16(b[0:2]) != uptimeMessageCodecID {
		return uptimeMessage{}, errNotUptimeMessage
	}
	if binary.BigEndian.Uint32(b[2:6]) != uptimeMessageTypeID {
		return uptimeMessage{}, errNotUptimeMessage
	}

	var msg uptimeMessage
	copy(msg.ValidationID[:], b[6:6+ids.IDLen])
	msg.UptimeSeconds = binary.BigEndian.Uint64(b[6+ids.IDLen:])
	return msg, nil
}

// parseUptimeAddressedCall extracts a ValidationUptimeMessage from the payload
// of an unsigned Warp message.
//
// It returns errNotUptimeMessage when the payload is not an uptime proof at
// all (e.g. it is one of this VM's outbound oracle messages), and a descriptive
// error when the payload *looks* like an uptime proof but violates a rule the
// StakingManager contract enforces — most importantly a non-empty source
// address, which would mean the proof did not come directly from a node.
func parseUptimeAddressedCall(warpPayload []byte) (uptimeMessage, error) {
	addressedCall, err := payload.ParseAddressedCall(warpPayload)
	if err != nil {
		return uptimeMessage{}, errNotUptimeMessage
	}

	msg, err := parseUptimeMessage(addressedCall.Payload)
	if err != nil {
		return uptimeMessage{}, err
	}

	// StakingManager requires originSenderAddress == address(0). Signing a
	// proof with a source address set would produce a message the contract
	// rejects anyway, so refuse it here rather than emit a useless signature.
	if len(addressedCall.SourceAddress) != 0 {
		return uptimeMessage{}, fmt.Errorf(
			"uptime proof must have an empty source address, got %d bytes",
			len(addressedCall.SourceAddress),
		)
	}

	return msg, nil
}

// newUptimeAddressedCall builds the AddressedCall payload for an uptime proof.
// Exposed for tests and for callers that want to construct the exact bytes a
// signature aggregator should request signatures over.
func newUptimeAddressedCall(validationID ids.ID, uptimeSeconds uint64) ([]byte, error) {
	inner := uptimeMessage{ValidationID: validationID, UptimeSeconds: uptimeSeconds}.marshal()
	addressedCall, err := payload.NewAddressedCall(nil, inner)
	if err != nil {
		return nil, fmt.Errorf("build uptime addressed call: %w", err)
	}
	return addressedCall.Bytes(), nil
}
