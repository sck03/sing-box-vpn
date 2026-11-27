package vpngate

// State is shared by the gateway, CLI and generated panel contract.
type State string

const (
	Fetching    State = "fetching"
	Connecting  State = "connecting"
	Verifying   State = "verifying"
	Waiting     State = "waiting"
	Ready       State = "ready"
	Stopped     State = "stopped"
	Unavailable State = "unavailable"
)

func States() []State {
	return []State{Fetching, Connecting, Verifying, Waiting, Ready, Stopped, Unavailable}
}
