package cloudflare

// RouteRoot represents the name of the route namespace
type RouteRoot string

const (
	// AccountRouteRoot is the accounts route namespace
	AccountRouteRoot RouteRoot = "accounts"

	// ZoneRouteRoot is the zones route namespace
	ZoneRouteRoot RouteRoot = "zones"

	// Used for testing
	testAccountID    = "01a7362d577a6c3019a474fd6f485823"
	testZoneID       = "d56084adb405e0b7e32c52321bf07be6"
	testCertPackUUID = "a77f8bd7-3b47-46b4-a6f1-75cf98109948"
)
