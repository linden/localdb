//go:build js && wasm

package localdb

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/btcsuite/btcwallet/walletdb/walletdbtest"
	"github.com/linden/tempdb"
)

func init() {
	// log every message, including debug level.
	tempdb.Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func TestInterface(t *testing.T) {
	walletdbtest.TestInterface(t, "localdb", "test.db")
}

func TestPersistence(t *testing.T) {
	// the name of the database.
	nm := "persistence.db"
	db, err := walletdb.Create("localdb", nm)
	if err != nil {
		t.Fatal(err)
	}

	// the name of the bucket.
	bktNm := []byte("alphabet")

	// the values to be set.
	vals := [][]byte{
		[]byte("a"),
		[]byte("b"),
		[]byte("c"),
	}

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		// create the bucket.
		bkt, err := tx.CreateTopLevelBucket(bktNm)
		if err != nil {
			return err
		}

		// set all the values.
		for _, c := range vals {
			// set the key and value to the same for simplicity.
			err = bkt.Put(c, c)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	db, err = walletdb.Open("localdb", nm)
	if err != nil {
		t.Fatal(err)
	}

	err = walletdb.View(db, func(tx walletdb.ReadTx) error {
		bkt := tx.ReadBucket(bktNm)

		for _, c := range vals {
			v := bkt.Get(c)
			if !bytes.Equal(c, v) {
				t.Fatalf("expected %v but got %v", c, v)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
