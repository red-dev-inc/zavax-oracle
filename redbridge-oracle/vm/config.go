package vm

// Config holds runtime parameters for the VM, deserialized from the
// configBytes passed to Initialize.
type Config struct {
	// ZcashRPCEndpoint is the URL of the Zcash JSON-RPC server (e.g.,
	// "http://127.0.0.1:8232").
	ZcashRPCEndpoint string `json:"zcashRpcEndpoint"`

	// ZcashRPCUser is the RPC username for authentication.
	ZcashRPCUser string `json:"zcashRpcUser"`

	// ZcashRPCPassword is the RPC password for authentication.
	ZcashRPCPassword string `json:"zcashRpcPassword"`

	// ConfirmationDepth is the number of Zcash blocks that must follow a
	// block before it is eligible for bridging. Defaults to 24 if zero.
	ConfirmationDepth uint64 `json:"confirmationDepth"`
}

// DefaultConfig returns a Config with production defaults.
func DefaultConfig() Config {
	return Config{
		ZcashRPCEndpoint:  "http://127.0.0.1:8232",
		ConfirmationDepth: 24,
	}
}

func (c *Config) applyDefaults() {
	if c.ConfirmationDepth == 0 {
		c.ConfirmationDepth = 24
	}
	if c.ZcashRPCEndpoint == "" {
		c.ZcashRPCEndpoint = "http://127.0.0.1:8232"
	}
}
