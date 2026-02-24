package side

import "strings"

type Side string

const (
	Client   Side = "CLIENT"
	Server   Side = "SERVER"
	Both     Side = "BOTH"
	ClientJ9 Side = "CLIENT_JAVA9"
	ServerJ9 Side = "SERVER_JAVA9"
	BothJ9   Side = "BOTH_JAVA9"
)

func Parse(s string) Side {
	return Side(strings.ToUpper(s))
}

// IncludedIn returns true if this side should be included for the given install side.
func (s Side) IncludedIn(installSide string) bool {
	installSide = strings.ToLower(installSide)
	switch s {
	case Both, BothJ9:
		return true
	case Client, ClientJ9:
		return installSide == "client"
	case Server, ServerJ9:
		return installSide == "server"
	default:
		return false
	}
}
