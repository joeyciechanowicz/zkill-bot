// Package sde holds EVE Static Data Export lookups compiled into the binary
// at build time. Types and SystemNames are regenerated from eve.db and ESI by
// the cmd/gen-sde and cmd/gen-systems tools.
package sde

// capitalGroupIDs is the set of EVE ship group IDs considered capital-class.
var capitalGroupIDs = map[int64]bool{
	30:   true, // Titan
	485:  true, // Dreadnought
	547:  true, // Carrier
	659:  true, // Supercarrier
	883:  true, // Capital Industrial Ship
	1538: true, // Force Auxiliary
}

// IsCapitalGroup reports whether groupID belongs to a capital-class ship.
func IsCapitalGroup(groupID int64) bool { return capitalGroupIDs[groupID] }

// LookupType returns the SDE entry for typeID and whether it was found.
func LookupType(typeID int64) (Type, bool) {
	t, ok := Types[typeID]
	return t, ok
}

// SystemName returns the in-game name of a solar system by ID, or "" if
// unknown.
func SystemName(systemID int64) string { return SystemNames[systemID] }
