package mssql

import (
	"github.com/google/uuid"
	mssqldriver "github.com/microsoft/go-mssqldb"
)

// guid converts between Go UUIDs and SQL Server's UNIQUEIDENTIFIER.
//
// The column stores the first three groups in little-endian order. Passing or
// scanning a bare uuid.UUID copies the raw bytes, which writes correctly but
// reads back reversed: the row is right and the Go value is wrong, so nothing
// fails until two ids are compared. The driver's type does the conversion, and
// routing every id through this alias keeps that from being forgotten at one
// call site.
type guid = mssqldriver.UniqueIdentifier

// asGUID prepares an id for a query parameter.
func asGUID(id uuid.UUID) guid { return guid(id) }
