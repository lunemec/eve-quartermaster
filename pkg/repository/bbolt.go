package repository

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

type BBoltRepository interface {
	Repository
	PriceHistory
	io.Closer
}

type bboltRepository struct {
	db *bolt.DB
}

var (
	doctrinesBucket    = []byte("doctrines")
	priceHistoryBucket = []byte("price_history")

	timeFormat = time.RFC3339
)

func NewBBoltRepository(databaseFile string) (BBoltRepository, error) {
	db, err := bolt.Open(databaseFile, 0600, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open DB file: %s", databaseFile)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(doctrinesBucket)
		if err != nil {
			return errors.Wrap(err, "unable to create doctrines bucket")
		}
		return nil
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(priceHistoryBucket)
		if err != nil {
			return errors.Wrap(err, "unable to create price_history bucket")
		}
		return nil
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &bboltRepository{
		db: db,
	}, nil
}

func (r *bboltRepository) Close() error {
	return r.db.Close()
}

func (r *bboltRepository) ReadAll() ([]Doctrine, error) {
	var out []Doctrine

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(doctrinesBucket)

		return b.ForEach(func(k, v []byte) error {
			var doctrine Doctrine
			err := json.Unmarshal(v, &doctrine)
			if err != nil {
				return errors.Wrap(err, "unable to unmarshal doctrine")
			}
			out = append(out, doctrine)

			return nil
		})
	})
	if err != nil {
		return nil, errors.Wrap(err, "error reading repository")
	}

	return out, nil
}

func (r *bboltRepository) WriteAll(requireStock []Doctrine) error {
	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(doctrinesBucket)
		// We save by name each doctrine we want for later cleanup
		// of leftovers.
		requiredDoctrines := make(map[string]struct{})

		// Insert all required doctrines.
		for _, doctrine := range requireStock {
			requiredDoctrines[doctrine.Name] = struct{}{}

			data, err := json.Marshal(&doctrine)
			if err != nil {
				return errors.Wrapf(err, "unable to marshal doctrine: %+v", doctrine)
			}
			err = b.Put([]byte(doctrine.Name), data)
			if err != nil {
				return errors.Wrap(err, "error in bucket.Put()")
			}
		}

		// Cleanup unwanted doctrines.
		var deleteKeys [][]byte
		b.ForEach(func(k []byte, _ []byte) error {
			_, ok := requiredDoctrines[string(k)]
			if !ok {
				deleteKeys = append(deleteKeys, k)
			}
			return nil
		})
		for _, deleteKey := range deleteKeys {
			err := b.Delete(deleteKey)
			if err != nil {
				return errors.Wrapf(err, "unable to delete: %+v", deleteKey)
			}
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "unable to write doctrines")
	}
	return r.db.Sync()
}

func (r *bboltRepository) Get(doctrineName string) (Doctrine, error) {
	var doctrine Doctrine
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(doctrinesBucket)

		data := b.Get([]byte(doctrineName))
		if data == nil {
			return ErrNotFound
		}
		err := json.Unmarshal(data, &doctrine)
		if err != nil {
			return errors.Wrapf(err, "error unmarshaling doctrine: %+v", data)
		}

		return nil
	})

	return doctrine, err
}

func (r *bboltRepository) Set(doctrineName string, doctrine Doctrine) error {
	// Setting requireStock to 0 means we want to delete the doctrine.
	if doctrine.RequireStock == 0 {
		err := r.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(doctrinesBucket)
			err := b.Delete([]byte(doctrineName))
			if err != nil {
				return errors.Wrapf(err, "unable to delete doctrine: %+v", doctrineName)
			}
			return nil
		})
		if err != nil {
			return errors.Wrap(err, "unable to Set doctrine to 0")
		}
		return nil
	}

	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(doctrinesBucket)
		data, err := json.Marshal(&doctrine)
		if err != nil {
			return errors.Wrapf(err, "unable to marshal doctrine: %+v", doctrine)
		}
		err = b.Put([]byte(doctrineName), data)
		if err != nil {
			return errors.Wrapf(err, "unable to Put doctrine: %+v", doctrine)
		}
		return nil
	})
	if err != nil {
		return errors.Wrap(err, "unable to Set doctrine")
	}
	return nil
}

func (r *bboltRepository) RecordPrice(pricedata PriceData) error {
	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(priceHistoryBucket)

		doctrineBucket, err := b.CreateBucketIfNotExists([]byte(pricedata.DoctrineName))
		if err != nil {
			return errors.Wrapf(err, "unable to create doctrine sub-bucket: %s", pricedata.DoctrineName)
		}

		key := pricedata.Timestamp.Format(timeFormat)
		data, err := json.Marshal(pricedata)
		if err != nil {
			return errors.Wrapf(err, "unable to encode price data: %+v", pricedata)
		}
		err = doctrineBucket.Put([]byte(key), data)
		if err != nil {
			return errors.Wrapf(err, "error saving price data: %s %+v", key, pricedata)
		}
		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "error recording price: %+v", pricedata)
	}

	return nil
}

func (r *bboltRepository) WriteAllPrices(pricesdata []PriceData) error {
	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(priceHistoryBucket)

		for _, pricedata := range pricesdata {
			doctrineBucket, err := b.CreateBucketIfNotExists([]byte(pricedata.DoctrineName))
			if err != nil {
				return errors.Wrapf(err, "unable to create doctrine sub-bucket: %s", pricedata.DoctrineName)
			}

			key := pricedata.Timestamp.Format(timeFormat)
			data, err := json.Marshal(pricedata)
			if err != nil {
				return errors.Wrapf(err, "unable to encode price data: %+v", pricedata)
			}
			err = doctrineBucket.Put([]byte(key), data)
			if err != nil {
				return errors.Wrapf(err, "error saving price data: %s %+v", key, pricedata)
			}
		}
		return nil
	})

	if err != nil {
		return errors.Wrap(err, "unable to write price history")
	}
	return r.db.Sync()
}

func (r *bboltRepository) SeekPrices(start time.Time, end time.Time) ([]PriceData, error) {
	var out []PriceData

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(priceHistoryBucket)

		err := b.ForEach(func(k, _ []byte) error {
			doctrineBucket := b.Bucket(k)
			c := doctrineBucket.Cursor()
			min := []byte(start.Format(timeFormat))
			max := []byte(end.Format(timeFormat))

			for k, data := c.Seek(min); k != nil && bytes.Compare(k, max) <= 0; k, data = c.Next() {
				var pricedata PriceData
				err := json.Unmarshal(data, &pricedata)
				if err != nil {
					return errors.Wrapf(err, "error unmarshaling price history data: %+v", string(data))
				}
				out = append(out, pricedata)
			}

			return nil
		})
		if err != nil {
			return errors.Wrap(err, "error iterating over doctrine buckets")
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to read price history")
	}
	return out, nil
}

func (r *bboltRepository) Prices() ([]PriceData, error) {
	var out []PriceData

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(priceHistoryBucket)

		err := b.ForEach(func(k, _ []byte) error {
			doctrineBucket := b.Bucket(k)

			err := doctrineBucket.ForEach(func(doctrineName, data []byte) error {
				var pricedata PriceData
				err := json.Unmarshal(data, &pricedata)
				if err != nil {
					return errors.Wrapf(err, "error unmarshaling price history data: %+v", string(data))
				}
				out = append(out, pricedata)
				return nil
			})
			if err != nil {
				return errors.Wrapf(err, "error iterating over price history of: %s", string(k))
			}
			return nil
		})
		if err != nil {
			return errors.Wrap(err, "error iterating over doctrine buckets")
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to read price history")
	}
	return out, nil
}

func (r *bboltRepository) NPricesForDoctrine(doctrineName string, n int) ([]PriceData, error) {
	var out []PriceData

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(priceHistoryBucket)
		doctrineBucket := b.Bucket([]byte(doctrineName))
		if doctrineBucket == nil {
			return nil
		}

		c := doctrineBucket.Cursor()
		for doctrineName, data := c.Last(); doctrineName != nil; doctrineName, data = c.Prev() {
			if n == 0 {
				return nil
			}

			var pricedata PriceData
			err := json.Unmarshal(data, &pricedata)
			if err != nil {
				return errors.Wrapf(err, "error unmarshaling price history data: %+v", string(data))
			}

			out = append(out, pricedata)
			n--
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to read price history")
	}

	return out, nil
}
