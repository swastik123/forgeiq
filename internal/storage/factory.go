package storage

import (
	"fmt"
	"strings"
)

// StorageType represents the type of storage backend
type StorageType string

const (
	StorageTypePostgres StorageType = "postgres"
	StorageTypeMongo    StorageType = "mongodb"
	StorageTypeMemory   StorageType = "memory"
)

// NewStorage creates a storage instance based on the connection string
func NewStorage(storageType string, dsn string) (Storage, error) {
	switch strings.ToLower(storageType) {
	case string(StorageTypePostgres):
		return NewPostgresStorage(dsn)
	case string(StorageTypeMemory):
		return NewMemoryStorage(), nil
	case string(StorageTypeMongo):
		return nil, fmt.Errorf("MongoDB storage not yet implemented")
	default:
		return nil, fmt.Errorf("unknown storage type: %s", storageType)
	}
}


