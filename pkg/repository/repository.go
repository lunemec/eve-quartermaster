package repository

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/pkg/errors"
)

type Repository interface {
	Read() ([]Doctrine, error)
	Write([]Doctrine) error
	Set(string, int, ContractedOn) error
}

type ContractedOn string

const (
	Alliance    ContractedOn = "alliance"
	Corporation ContractedOn = "corporation"
)

type Doctrine struct {
	Name         string       `json:"name"`
	WantInStock  int          `json:"want_in_stock"`
	ContractedOn ContractedOn `json:"contracted_on"`
}

type jsonRepository struct {
	filename string

	lock sync.Mutex
}

func NewJSONRepository(filename string) (Repository, error) {
	return &jsonRepository{
		filename: filename,
	}, nil
}

func (r *jsonRepository) Read() ([]Doctrine, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	f, err := os.OpenFile(r.filename, os.O_RDONLY, 0775)
	if err != nil {
		if os.IsNotExist(err) {
			return []Doctrine{}, nil
		}
		return nil, errors.Wrap(err, "error opening repository file")
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, errors.Wrap(err, "error reading repository file")
	}
	var out []Doctrine
	err = json.Unmarshal(data, &out)
	if err != nil {
		return nil, errors.Wrap(err, "error decoding repository file")
	}

	return out, nil
}

func (r *jsonRepository) Write(wantInStock []Doctrine) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	f, err := os.OpenFile(r.filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0775)
	if err != nil {
		return errors.Wrap(err, "error opening repository file")
	}
	defer f.Close()

	data, err := json.Marshal(wantInStock)
	if err != nil {
		return errors.Wrap(err, "error encoding repository file")
	}

	n, err := f.Write(data)
	if err != nil {
		return errors.Wrap(err, "error writing data to repository file")
	}
	if n != len(data) {
		return errors.Errorf("wrote less bytes (%d) than should (%d)", n, len(data))
	}

	return nil
}

func (r *jsonRepository) Set(doctrineName string, wantInStock int, contractOn ContractedOn) error {
	updatedDoctrines, err := r.Read()
	if err != nil {
		return errors.Wrap(err, "error reading current saved doctrines")
	}
	var (
		found     bool
		remove    bool
		removeIdx int
	)

	if wantInStock == 0 {
		remove = true
	}

	for i, savedDoctrine := range updatedDoctrines {
		if savedDoctrine.Name == doctrineName {
			found = true
			removeIdx = i

			savedDoctrine.WantInStock = wantInStock
			savedDoctrine.ContractedOn = contractOn
			updatedDoctrines[i] = savedDoctrine
		}
	}
	if remove {
		updatedDoctrines = append(updatedDoctrines[:removeIdx], updatedDoctrines[removeIdx+1:]...)
	}
	if !found && !remove {
		updatedDoctrines = append(updatedDoctrines, Doctrine{
			Name:         doctrineName,
			WantInStock:  wantInStock,
			ContractedOn: contractOn,
		})
	}
	return r.Write(updatedDoctrines)
}
