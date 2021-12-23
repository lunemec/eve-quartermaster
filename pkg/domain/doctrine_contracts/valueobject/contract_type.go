package valueobject

// ContractAvailability describes who the contract is available to.
type ContractAvailability string

const (
	// Contract is available to Alliance.
	ContractAvailabilityAlliance ContractAvailability = "alliance"
	// Contract is available to Corporation.
	ContractAvailabilityCorporation ContractAvailability = "corporation"
)
