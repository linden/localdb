//go:build js && wasm

package localdb

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/linden/indexeddb"
	"github.com/linden/tempdb"
)

const (
	// the name of the object store for the buckets.
	bucketStore = "buckets"

	// the version of the indexeddb database.
	version = 1
)

// share a logger with tempdb.
var Logger = tempdb.Logger

type DB struct {
	idb *indexeddb.DB
	*tempdb.DB
}

func (db *DB) BeginReadWriteTx() (walletdb.ReadWriteTx, error) {
	// create the transaction.
	tx, err := db.DB.BeginReadWriteTx()
	if err != nil {
		return nil, err
	}

	// cast to a TempDB transaction so we can access the state.
	ttx := tx.(*tempdb.Transaction)

	// add a commit hook to update the state.
	// TODO: handle errors.
	tx.OnCommit(func() {
		// create a new read/write transaction.
		itx, err := db.idb.NewTransaction([]string{bucketStore}, indexeddb.ReadWriteMode)
		if err != nil {
			panic(err)
		}

		// open the bucket store.
		btch := itx.Store(bucketStore).Batch()

		// save every bucket by index.
		for i, bkt := range ttx.State.Buckets {
			// create a buffer.
			buf := new(bytes.Buffer)

			// encode the bucket into the buffer.
			err = gob.NewEncoder(buf).Encode(&bkt)
			if err != nil {
				panic(err)
			}

			// quote the string, since Go strings aren't UTF-8.
			// https://go.dev/blog/strings.
			v := strconv.Quote(buf.String())

			err = btch.Put(i, v)
			if err != nil {
				panic(err)
			}
		}

		err = btch.Wait()
		if err != nil {
			panic(err)
		}
	})

	return tx, nil
}

// we need to override `tempdb.Update` here so we can ensure we call our `BeginReadWriteTx` and our update hook is added.
func (db *DB) Update(fn func(tx walletdb.ReadWriteTx) error, reset func()) error {
	reset()

	// create a new transaction.
	tx, err := db.BeginReadWriteTx()
	if err != nil {
		return err
	}

	// call the function.
	err = fn(tx)
	if err != nil {
		tx.Rollback()
		return err
	}

	// cast to a TempDB transaction so we can access the rollback status.
	ttx := tx.(*tempdb.Transaction)

	// ensure the transaciton has not been rolledback.
	if ttx.Rolledback {
		return nil
	}

	return tx.Commit()
}

func newDB(create bool, args ...any) (*DB, error) {
	// create the undelying tempDB database.
	db, err := tempdb.New(args...)
	if err != nil {
		return nil, err
	}

	// cast the database to tempDB database.
	tdb := db.(*tempdb.DB)

	// wether or not the database existed before calling this function.
	exist := true

	// use the path as the database name.
	idb, err := indexeddb.New(tdb.Path, 1, func(up *indexeddb.Upgrade) error {
		// create the buckets store.
		up.CreateStore(bucketStore)

		exist = false

		return nil
	})
	if err != nil {
		return nil, err
	}

	// ensure the database did not already exist when creating.
	if create && exist {
		return nil, walletdb.ErrDbExists
	}

	// ensure the database exists when opening.
	if !create && !exist {
		return nil, walletdb.ErrDbDoesNotExist
	}

	return &DB{
		idb: idb,
		DB:  tdb,
	}, nil
}

// create a new database.
func New(args ...any) (walletdb.DB, error) {
	return newDB(true, args...)
}

// open an existing database.
func Open(args ...any) (walletdb.DB, error) {
	db, err := newDB(false, args...)
	if err != nil {
		return nil, err
	}

	// create a read transaction.
	itx, err := db.idb.NewTransaction([]string{bucketStore}, indexeddb.ReadMode)
	if err != nil {
		return nil, err
	}

	// open the buckets store.
	bkts := itx.Store(bucketStore)

	count, err := bkts.Count()
	if err != nil {
		return nil, err
	}

	state := &tempdb.State{}

	for i := 0; i < count; i++ {
		// get the encoded bucket.
		val, err := bkts.Get(i)
		if err != nil {
			return nil, err
		}

		// ensure the value is a string.
		if t := val.Type(); t != js.TypeString {
			return nil, fmt.Errorf("expected a type of %s: got %s", js.TypeString, t)
		}

		// unquote the string.
		raw, err := strconv.Unquote(val.String())
		if err != nil {
			return nil, err
		}

		// create a reader for the encoded bucket.
		r := strings.NewReader(raw)

		var bkt tempdb.Bucket

		// decode the bucket.
		err = gob.NewDecoder(r).Decode(&bkt)
		if err != nil {
			return nil, err
		}

		// add the bucket to the state.
		state.Buckets = append(state.Buckets, bkt)
	}

	// update the database state.
	*db.State = *state

	return db, nil
}

func init() {
	err := walletdb.RegisterDriver(walletdb.Driver{
		DbType: "localdb",

		Create: New,
		Open:   Open,
	})

	if err != nil {
		panic(err)
	}
}
