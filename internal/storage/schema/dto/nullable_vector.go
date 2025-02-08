package dto

import (
	"database/sql"
	"database/sql/driver"

	"github.com/pgvector/pgvector-go"
)

type NullableVector struct {
	sql.Null[pgvector.Vector]
}

func (nv *NullableVector) Scan(src any) error {
	if src == nil {
		nv.V = pgvector.Vector{}
		nv.Valid = false
		return nil
	}

	nv.Valid = true
	return nv.V.Scan(src)
}

// Value implements the driver.Valuer interface.
func (nv NullableVector) Value() (driver.Value, error) {
	if !nv.Valid {
		return nil, nil
	}

	return nv.V.Value()
}
