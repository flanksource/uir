package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flanksource/commons-db/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DatabaseModule registers one database under a reusable connection before its
// first catalogue snapshot has succeeded.
type DatabaseModule struct {
	ID           uuid.UUID `gorm:"column:id;primaryKey" json:"id"`
	ConnectionID uuid.UUID `gorm:"column:connection_id" json:"connection_id"`
	DatabaseName string    `gorm:"column:database_name" json:"database_name"`
	Name         string    `gorm:"column:name" json:"name"`
	RootKey      string    `gorm:"column:root_key" json:"root_key"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
}

func (DatabaseModule) TableName() string { return "database_modules" }

func ListConnections(ctx context.Context, database *gorm.DB) ([]models.Connection, error) {
	var connections []models.Connection
	err := database.WithContext(ctx).Table("connections").Order("name, id").Find(&connections).Error
	return connections, err
}

func GetConnection(ctx context.Context, database *gorm.DB, id uuid.UUID) (models.Connection, error) {
	var connection models.Connection
	err := database.WithContext(ctx).Table("connections").Where("id = ?", id).First(&connection).Error
	return connection, err
}

func CreateConnection(ctx context.Context, database *gorm.DB, connection *models.Connection) error {
	if connection.ID == uuid.Nil {
		connection.ID = uuid.New()
	}
	now := time.Now().UTC()
	connection.CreatedAt, connection.UpdatedAt = now, now
	return database.WithContext(ctx).Table("connections").Create(map[string]any{
		"id": connection.ID, "name": connection.Name, "namespace": connection.Namespace,
		"type": connection.Type, "url": connection.URL, "username": connection.Username,
		"password": connection.Password, "certificate": connection.Certificate,
		"properties": connection.Properties, "insecure_tls": connection.InsecureTLS,
		"created_at": now, "updated_at": now,
	}).Error
}

func UpdateConnection(ctx context.Context, database *gorm.DB, connection models.Connection) error {
	result := database.WithContext(ctx).Table("connections").Where("id = ?", connection.ID).Updates(map[string]any{
		"name": connection.Name, "namespace": connection.Namespace, "type": connection.Type,
		"url": connection.URL, "username": connection.Username, "password": connection.Password,
		"certificate": connection.Certificate, "properties": connection.Properties,
		"insecure_tls": connection.InsecureTLS, "updated_at": time.Now().UTC(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteConnection(ctx context.Context, database *gorm.DB, id uuid.UUID) error {
	var dependents int64
	if err := database.WithContext(ctx).Model(&DatabaseModule{}).Where("connection_id = ?", id).Count(&dependents).Error; err != nil {
		return err
	}
	if dependents > 0 {
		return fmt.Errorf("connection %s has %d database modules", id, dependents)
	}
	result := database.WithContext(ctx).Table("connections").Where("id = ?", id).Delete(&models.Connection{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func ListDatabaseModules(ctx context.Context, database *gorm.DB) ([]DatabaseModule, error) {
	var modules []DatabaseModule
	err := database.WithContext(ctx).Order("name, id").Find(&modules).Error
	return modules, err
}

func GetDatabaseModule(ctx context.Context, database *gorm.DB, id uuid.UUID) (DatabaseModule, error) {
	var module DatabaseModule
	err := database.WithContext(ctx).Where("id = ?", id).First(&module).Error
	return module, err
}

func CreateDatabaseModule(ctx context.Context, database *gorm.DB, module *DatabaseModule) error {
	if module == nil {
		return errors.New("database module is required")
	}
	if module.ID == uuid.Nil {
		module.ID = uuid.New()
	}
	if module.RootKey == "" {
		module.RootKey = "database/" + module.ID.String()
	}
	module.CreatedAt = time.Now().UTC()
	return database.WithContext(ctx).Create(module).Error
}

func DeleteDatabaseModule(ctx context.Context, database *gorm.DB, id uuid.UUID) error {
	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		module, err := GetDatabaseModule(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Delete(&module).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Where("root_key = ?", module.RootKey).Delete(&ModuleRoot{}).Error
	})
}
