// Code generated by genji.
// DO NOT EDIT!

package genji_test

import (
	"errors"

	"github.com/asdine/genji/field"
	"github.com/asdine/genji/index"
	"github.com/asdine/genji/query"
	"github.com/asdine/genji/record"
)

// GetField implements the field method of the record.Record interface.
func (u *User) GetField(name string) (field.Field, error) {
	switch name {
	case "ID":
		return field.NewInt64("ID", u.ID), nil
	case "Name":
		return field.NewString("Name", u.Name), nil
	case "Age":
		return field.NewUint32("Age", u.Age), nil
	}

	return field.Field{}, errors.New("unknown field")
}

// Iterate through all the fields one by one and pass each of them to the given function.
// It the given function returns an error, the iteration is interrupted.
func (u *User) Iterate(fn func(field.Field) error) error {
	var err error

	err = fn(field.NewInt64("ID", u.ID))
	if err != nil {
		return err
	}

	err = fn(field.NewString("Name", u.Name))
	if err != nil {
		return err
	}

	err = fn(field.NewUint32("Age", u.Age))
	if err != nil {
		return err
	}

	return nil
}

// ScanRecord extracts fields from record and assigns them to the struct fields.
// It implements the record.Scanner interface.
func (u *User) ScanRecord(rec record.Record) error {
	return rec.Iterate(func(f field.Field) error {
		var err error

		switch f.Name {
		case "ID":
			u.ID, err = field.DecodeInt64(f.Data)
		case "Name":
			u.Name, err = field.DecodeString(f.Data)
		case "Age":
			u.Age, err = field.DecodeUint32(f.Data)
		}
		return err
	})
}

// PrimaryKey returns the primary key. It implements the table.PrimaryKeyer interface.
func (u *User) PrimaryKey() ([]byte, error) {
	return field.EncodeInt64(u.ID), nil
}

// UserFields describes the fields of the User record.
// It can be used to select fields during queries.
type UserFields struct {
	ID   query.Int64FieldSelector
	Name query.StringFieldSelector
	Age  query.Uint32FieldSelector
}

// NewUserFields creates a UserFields.
func NewUserFields() *UserFields {
	return &UserFields{
		ID:   query.Int64Field("ID"),
		Name: query.StringField("Name"),
		Age:  query.Uint32Field("Age"),
	}
}

// NewUserIndexes creates a map containing the configuration for each index of the table.
func NewUserIndexes() map[string]index.Options {
	return map[string]index.Options{
		"Name": index.Options{Unique: false},
	}
}
