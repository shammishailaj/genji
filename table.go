package genji

import (
	"strings"
	"time"

	"github.com/asdine/genji/index"

	"github.com/asdine/genji/engine"
	"github.com/asdine/genji/field"
	"github.com/asdine/genji/record"
	"github.com/asdine/genji/table"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
)

// A Table represents a collection of records.
type Table struct {
	tx    *Tx
	store engine.Store
	name  string
}

// Iterate goes through all the records of the table and calls the given function by passing each one of them.
// If the given function returns an error, the iteration stops.
func (t Table) Iterate(fn func(recordID []byte, r record.Record) error) error {
	// To avoid unnecessary allocations, we create the slice once and reuse it
	// at each call of the fn method.
	// Since the AscendGreaterOrEqual is never supposed to call the callback concurrently
	// we can assume that it's thread safe.
	// TODO(asdine) Add a mutex if proven necessary
	var r record.EncodedRecord

	return t.store.AscendGreaterOrEqual(nil, func(recordID, v []byte) error {
		r = v
		// r must be passed as pointer, not value, because passing a value to an interface
		// requires an allocation, while it doesn't for a pointer.
		return fn(recordID, &r)
	})
}

// GetRecord returns one record by recordID. It implements the table.RecordGetter interface.
func (t Table) GetRecord(recordID []byte) (record.Record, error) {
	v, err := t.store.Get(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return nil, table.ErrRecordNotFound
		}
		return nil, errors.Wrapf(err, "failed to fetch record %q", recordID)
	}

	return record.EncodedRecord(v), err
}

// Insert the record into the table.
// If the record implements the table.Pker interface, it will be used to generate a recordID,
// otherwise it will be generated automatically. Note that there are no ordering guarantees
// regarding the recordID generated by default.
func (t Table) Insert(r record.Record) ([]byte, error) {
	v, err := record.Encode(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode record")
	}

	var recordID []byte
	if pker, ok := r.(table.PrimaryKeyer); ok {
		recordID, err = pker.PrimaryKey()
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate recordID from PrimaryKey method")
		}
	} else {
		id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
		if err == nil {
			recordID, err = id.MarshalText()
		}
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate recordID")
		}
	}

	_, err = t.store.Get(recordID)
	if err == nil {
		return nil, table.ErrDuplicate
	}

	err = t.store.Put(recordID, v)
	if err != nil {
		return nil, err
	}

	indexes, err := t.Indexes()
	if err != nil {
		return nil, err
	}

	for fieldName, idx := range indexes {
		f, err := r.GetField(fieldName)
		if err != nil {
			return nil, err
		}

		err = idx.Set(f.Data, recordID)
		if err != nil {
			if err == index.ErrDuplicate {
				return nil, table.ErrDuplicate
			}

			return nil, err
		}
	}

	return recordID, nil
}

// Delete a record by recordID.
// Indexes are automatically updated.
func (t Table) Delete(recordID []byte) error {
	err := t.store.Delete(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return table.ErrRecordNotFound
		}
		return err
	}

	indexes, err := t.Indexes()
	if err != nil {
		return err
	}

	for _, idx := range indexes {
		err = idx.Delete(recordID)
		if err != nil {
			return err
		}
	}

	return nil
}

type pkWrapper struct {
	record.Record
	pk []byte
}

func (p pkWrapper) PrimaryKey() ([]byte, error) {
	return p.pk, nil
}

// Replace a record by recordID.
// An error is returned if the recordID doesn't exist.
// Indexes are automatically updated.
func (t Table) Replace(recordID []byte, r record.Record) error {
	err := t.Delete(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return table.ErrRecordNotFound
		}
		return err
	}

	_, err = t.Insert(pkWrapper{Record: r, pk: recordID})
	return err
}

// Truncate deletes all the records from the table.
func (t Table) Truncate() error {
	return t.store.Truncate()
}

// AddField changes the table structure by adding a field to all the records.
// If the field data is empty, it is filled with the zero value of the field type.
// If a record already has the field, no change is performed on that record.
func (t Table) AddField(f field.Field) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		if _, err = fb.GetField(f.Name); err == nil {
			// if the field already exists, skip
			return nil
		}

		if f.Data == nil {
			f.Data = field.ZeroValue(f.Type).Data
		}
		fb.Add(f)

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

// DeleteField changes the table structure by deleting a field from all the records.
func (t Table) DeleteField(name string) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID []byte, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		err = fb.Delete(name)
		if err != nil {
			// if the field doesn't exist, skip
			return nil
		}

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

// RenameField changes the table structure by renaming the selected field on all the records.
func (t Table) RenameField(oldName, newName string) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID []byte, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		f, err := fb.GetField(oldName)
		if err != nil {
			// if the field doesn't exist, skip
			return nil
		}

		f.Name = newName
		fb.Replace(oldName, f)

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

func buildIndexName(tableName, field string) string {
	var b strings.Builder
	b.WriteString(indexPrefix)
	b.WriteString(tableName)
	b.WriteByte(separator)
	b.WriteString(field)

	return b.String()
}

// CreateIndex creates an index with the given name.
// If it already exists, returns ErrTableAlreadyExists.
func (t Table) CreateIndex(field string, opts index.Options) (index.Index, error) {
	it, err := t.tx.Table(indexTable)
	if err != nil {
		return nil, err
	}

	idxName := buildIndexName(t.name, field)

	_, err = it.GetRecord([]byte(idxName))
	if err == nil {
		return nil, ErrIndexAlreadyExists
	}
	if err != table.ErrRecordNotFound {
		return nil, err
	}

	_, err = it.Insert(&indexOptions{
		TableName: t.name,
		FieldName: field,
		Unique:    opts.Unique,
	})
	if err != nil {
		return nil, err
	}

	err = t.tx.tx.CreateStore(idxName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create index %q on table %q", field, t.name)
	}

	s, err := t.tx.tx.Store(idxName)
	if err == engine.ErrStoreNotFound {
		return nil, ErrIndexNotFound
	}
	if err != nil {
		return nil, err
	}

	return index.New(s, index.Options{Unique: opts.Unique}), nil
}

// CreateIndexIfNotExists calls CreateIndex and returns no error if it already exists.
func (t Table) CreateIndexIfNotExists(field string, opts index.Options) (index.Index, error) {
	idx, err := t.CreateIndex(field, opts)
	if err == nil {
		return idx, nil
	}
	if err == ErrIndexAlreadyExists {
		return t.GetIndex(field)
	}

	return nil, err
}

// CreateIndexesIfNotExist takes a map that associates field names to index options
// and ensures these indexes are created if they don't already exist.
// This method doesn't reindex the table if a new index is created.
func (t Table) CreateIndexesIfNotExist(indexes map[string]index.Options) error {
	for fieldName, idxOpts := range indexes {
		_, err := t.CreateIndexIfNotExists(fieldName, idxOpts)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetIndex returns an index by name.
func (t Table) GetIndex(field string) (index.Index, error) {
	indexName := buildIndexName(t.name, field)

	opts, err := readIndexOptions(t.tx, indexName)
	if err != nil {
		return nil, err
	}

	s, err := t.tx.tx.Store(indexName)
	if err == engine.ErrStoreNotFound {
		return nil, ErrIndexNotFound
	}
	if err != nil {
		return nil, err
	}

	return index.New(s, index.Options{Unique: opts.Unique}), nil
}

// Indexes returns a map of all the indexes of a table.
func (t Table) Indexes() (map[string]index.Index, error) {
	prefix := buildIndexName(t.name, "")
	list, err := t.tx.tx.ListStores(prefix)
	if err != nil {
		return nil, err
	}

	indexes := make(map[string]index.Index)
	for _, storeName := range list {
		idxName := strings.TrimPrefix(storeName, prefix)
		indexes[idxName], err = t.GetIndex(idxName)
		if err != nil {
			return nil, err
		}
	}

	return indexes, nil
}

// DropIndex deletes an index from the database.
func (t Table) DropIndex(field string) error {
	it, err := t.tx.Table(indexTable)
	if err != nil {
		return err
	}

	indexName := buildIndexName(t.name, field)
	err = it.Delete([]byte(indexName))
	if err == table.ErrRecordNotFound {
		return ErrIndexNotFound
	}
	if err != nil {
		return err
	}

	err = t.tx.tx.DropStore(indexName)
	if err == engine.ErrStoreNotFound {
		return ErrIndexNotFound
	}
	return err
}

// ReIndex drops the selected index, creates a new one and runs over all the records
// to fill the newly created index.
func (t Table) ReIndex(fieldName string) error {
	err := t.DropIndex(fieldName)
	if err != nil {
		return err
	}

	indexName := buildIndexName(t.name, fieldName)

	opts, err := readIndexOptions(t.tx, indexName)
	if err != nil {
		return err
	}

	idx, err := t.CreateIndex(fieldName, index.Options{Unique: opts.Unique})
	if err != nil {
		return err
	}

	return t.Iterate(func(recordID []byte, r record.Record) error {
		f, err := r.GetField(fieldName)
		if err != nil {
			return err
		}

		return idx.Set(f.Data, recordID)
	})
}

// SelectTable returns the current table. Implements the query.TableSelector interface.
func (t Table) SelectTable(*Tx) (*Table, error) {
	return &t, nil
}

// TableName returns the name of the table.
func (t Table) TableName() string {
	return t.name
}
