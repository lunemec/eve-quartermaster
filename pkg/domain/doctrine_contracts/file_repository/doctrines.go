package filerepository

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/lunemec/eve-quartermaster/pkg/domain/doctrine_contracts/aggregate"

	"github.com/pkg/errors"
)

type jsonDoctrinesRepository struct {
	filename string

	lock sync.Mutex
}

func NewJSONDoctrinesRepository(filename string) (*jsonDoctrinesRepository, error) {
	return &jsonDoctrinesRepository{
		filename: filename,
	}, nil
}

func (r *jsonDoctrinesRepository) Read() ([]aggregate.DoctrineContractCheck, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	f, err := os.OpenFile(r.filename, os.O_RDONLY, 0775)
	if err != nil {
		if os.IsNotExist(err) {
			return []aggregate.DoctrineContractCheck{}, nil
		}
		return nil, errors.Wrap(err, "error opening DoctrinesRepository file")
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "error reading DoctrinesRepository file")
	}
	var out []aggregate.DoctrineContractCheck
	err = json.Unmarshal(data, &out)
	if err != nil {
		return nil, errors.Wrap(err, "error decoding DoctrinesRepository file")
	}

	return out, nil
}

func (r *jsonDoctrinesRepository) Write(requireStock []aggregate.DoctrineContractCheck) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	f, err := os.OpenFile(r.filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0775)
	if err != nil {
		return errors.Wrap(err, "error opening DoctrinesRepository file")
	}
	defer f.Close()

	data, err := json.Marshal(requireStock)
	if err != nil {
		return errors.Wrap(err, "error encoding DoctrinesRepository file")
	}

	n, err := f.Write(data)
	if err != nil {
		return errors.Wrap(err, "error writing data to DoctrinesRepository file")
	}
	if n != len(data) {
		return errors.Errorf("wrote less bytes (%d) than should (%d)", n, len(data))
	}

	return nil
}

func (r *jsonDoctrinesRepository) Set(doctrine aggregate.DoctrineContractCheck) error {
	updatedDoctrines, err := r.Read()
	if err != nil {
		return errors.Wrap(err, "error reading current saved doctrines")
	}
	var (
		found     bool
		remove    bool
		removeIdx int
	)

	if doctrine.ContractCount == 0 {
		remove = true
	}

	for i, savedDoctrine := range updatedDoctrines {
		if savedDoctrine.Name == doctrine.Name {
			found = true
			removeIdx = i

			savedDoctrine.ContractCount = doctrine.ContractCount
			savedDoctrine.ContractAvailability = doctrine.ContractAvailability
			updatedDoctrines[i] = savedDoctrine
		}
	}
	if remove {
		updatedDoctrines = append(updatedDoctrines[:removeIdx], updatedDoctrines[removeIdx+1:]...)
	}
	if !found && !remove {
		updatedDoctrines = append(updatedDoctrines, doctrine)
	}
	return r.Write(updatedDoctrines)
}
