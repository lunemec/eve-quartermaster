package repository

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
)

type Repository interface {
	ReadAll() ([]Doctrine, error)
	WriteAll([]Doctrine) error
	Get(string) (Doctrine, error)
	Set(string, Doctrine) error
}

type ContractedOn string

const (
	Alliance    ContractedOn = "alliance"
	Corporation ContractedOn = "corporation"
)

type Doctrine struct {
	Name         string        `json:"name"`           // Name of the doctrine.
	RequireStock int           `json:"require_stock"`  // How many to have on contract.
	ContractedOn ContractedOn  `json:"contracted_on"`  // Alliance/Corporation contract.
	Price        DoctrinePrice `json:"doctrine_price"` // Price details.
}

type DoctrinePrice struct {
	Buy       uint64    `json:"buy"`       // How much was this ship bought for.
	Timestamp time.Time `json:"timestamp"` // When.
}

type PriceHistory interface {
	RecordPrice(PriceData) error
	SeekPrices(start time.Time, end time.Time) ([]PriceData, error)
	Prices() ([]PriceData, error)
	WriteAllPrices([]PriceData) error
}

type PriceData struct {
	Timestamp    time.Time `json:"timestamp"`
	DoctrineName string    `json:"doctrine_name"`
	ContractID   int32     `json:"contract_id"`
	IssuerID     int32     `json:"issuer_id"`
	Price        uint64    `json:"price"`
}

var ErrNotFound = errors.New("doctrine not found")

// deprecated: jsonRepository must be migrated to bbolt repository.
type jsonRepository struct {
	filename string

	lock sync.Mutex
}

func NewJSONRepository(filename string) (Repository, error) {
	return &jsonRepository{
		filename: filename,
	}, nil
}

func (r *jsonRepository) ReadAll() ([]Doctrine, error) {
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

func (r *jsonRepository) WriteAll(requireStock []Doctrine) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	f, err := os.OpenFile(r.filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0775)
	if err != nil {
		return errors.Wrap(err, "error opening repository file")
	}
	defer f.Close()

	data, err := json.Marshal(requireStock)
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

func (r *jsonRepository) Get(doctrineName string) (Doctrine, error) {
	doctrines, err := r.ReadAll()
	if err != nil {
		return Doctrine{}, errors.Wrap(err, "error reading current saved doctrines")
	}

	for _, savedDoctrine := range doctrines {
		if savedDoctrine.Name == doctrineName {
			return savedDoctrine, nil
		}
	}
	return Doctrine{}, ErrNotFound
}

func (r *jsonRepository) Set(doctrineName string, doctrine Doctrine) error {
	updatedDoctrines, err := r.ReadAll()
	if err != nil {
		return errors.Wrap(err, "error reading current saved doctrines")
	}
	var (
		found     bool
		remove    bool
		removeIdx int
	)

	if doctrine.RequireStock == 0 {
		remove = true
	}

	for i, savedDoctrine := range updatedDoctrines {
		if savedDoctrine.Name == doctrineName {
			found = true
			removeIdx = i

			updatedDoctrines[i] = doctrine
			break
		}
	}
	if remove {
		updatedDoctrines = append(updatedDoctrines[:removeIdx], updatedDoctrines[removeIdx+1:]...)
	}
	if !found && !remove {
		updatedDoctrines = append(updatedDoctrines, doctrine)
	}
	return r.WriteAll(updatedDoctrines)
}
