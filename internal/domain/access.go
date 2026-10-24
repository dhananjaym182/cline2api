package domain

// AccessClass is the access basis of one model on one route. It is evaluated
// per provider/account route, never as a global property of a model ID.
type AccessClass string

const (
	AccessFree         AccessClass = "free"
	AccessFreeTier     AccessClass = "free_tier"
	AccessSubscription AccessClass = "subscription"
	AccessPaidAPI      AccessClass = "paid_api"
	AccessTrial        AccessClass = "trial"
	AccessUnknown      AccessClass = "unknown"
)

// Valid reports whether c is a defined access class.
func (c AccessClass) Valid() bool {
	switch c {
	case AccessFree, AccessFreeTier, AccessSubscription, AccessPaidAPI, AccessTrial, AccessUnknown:
		return true
	}
	return false
}

// EligibleForFreeOnly reports whether a route may be used by Free-only routing.
// A free_tier route qualifies only when the selected account/route has a
// verified active allowance. Unknown and unverified classes never qualify.
func (c AccessClass) EligibleForFreeOnly(allowanceVerified bool) bool {
	switch c {
	case AccessFree:
		return true
	case AccessFreeTier:
		return allowanceVerified
	default:
		return false
	}
}

// AccessClassForSignal maps an import classifier signal to an access class.
// Provider-level free flags are metadata, so they map to unknown rather than
// free: the route must be verified before Free-only routing may use it.
func AccessClassForSignal(signal string) AccessClass {
	switch signal {
	case "pricing-zero", "flag-free", "suffix-free", "name-token-free", "display-name-free", "allowlist":
		return AccessFree
	case "pricing-paid":
		return AccessPaidAPI
	default:
		return AccessUnknown
	}
}
