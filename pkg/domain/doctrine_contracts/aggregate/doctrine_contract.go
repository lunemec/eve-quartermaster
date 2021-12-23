package aggregate

import (
	"github.com/lunemec/eve-quartermaster/pkg/domain/doctrine_contracts/entity"
	"github.com/lunemec/eve-quartermaster/pkg/domain/doctrine_contracts/valueobject"
)

// DoctrineContractCheck describes Doctrine on contract to be checked.
type DoctrineContractCheck struct {
	Name                 entity.Doctrine                  `json:"name"`
	ContractCount        valueobject.ContractCount        `json:"require_stock"`
	ContractAvailability valueobject.ContractAvailability `json:"contracted_on"`
}
