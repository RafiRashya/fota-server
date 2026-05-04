package models

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base struct untuk tabel yang menggunakan UUID sebagai Primary Key
type Base struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
}

type User struct {
	Base
	Email        string    `gorm:"type:varchar(255);unique;not null" json:"email"`
	PasswordHash string    `gorm:"type:varchar(255);not null" json:"-"`
	Role         string    `gorm:"type:varchar(50);not null" json:"role"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
}

type RefreshToken struct {
    Base
    TokenHash string    `gorm:"type:varchar(255);not null" json:"token_hash"`
    UserID    uuid.UUID `gorm:"type:uuid;not null" json:"user_id"`
    ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
    Revoked   bool      `gorm:"not null;default:false" json:"revoked"`

	User      User      `gorm:"foreignKey:UserID" json:"-"`
}

// Validasi role sebelum simpan
func (u *User) BeforeSave(tx *gorm.DB) error {
    if u.Role != "ADMIN" && u.Role != "USER" {
        return gorm.ErrInvalidData
    }
    return nil
}

type Gateway struct {
	Base
	MacAddress string    `gorm:"type:varchar(17);unique;not null" json:"mac_address"`
	Name       string    `gorm:"type:varchar(100);not null" json:"name"`
	Location   *string   `gorm:"type:varchar(255)" json:"location"`
	Status     *string   `gorm:"type:varchar(50);default:'OFFLINE'" json:"status"`
	LastSeen   *time.Time `json:"last_seen"`
	CreatedAt  time.Time `json:"created_at"`
	Nodes      []Node    `gorm:"foreignKey:GatewayID" json:"nodes,omitempty"` // Relasi 1-to-N
}

type Firmware struct {
	Base
	Version      string    `gorm:"type:varchar(50);unique;not null" json:"version"`
	FileName     string    `gorm:"type:varchar(255);not null" json:"file_name"`
	FileSize     int       `gorm:"not null" json:"file_size"`
	Checksum     string    `gorm:"type:varchar(255);not null" json:"checksum"`
	ReleaseNotes *string   `gorm:"type:text" json:"release_notes"`
	UploadedAt   time.Time `json:"uploaded_at"`
}

type Node struct {
	Base
	GatewayID         uuid.UUID `gorm:"type:uuid;not null" json:"gateway_id"`
	MacAddress        string    `gorm:"type:varchar(17);unique;not null" json:"mac_address"`
	Name              string    `gorm:"type:varchar(100);not null" json:"name"`
	CurrentFirmwareID *uuid.UUID `gorm:"type:uuid" json:"current_firmware_id"`
	Status            *string   `gorm:"type:varchar(50);default:'OFFLINE'" json:"status"`
	LastVbatt         *float64  `json:"last_vbatt"`
	UpdatedAt         time.Time `json:"updated_at"`
	CreatedAt         time.Time `json:"created_at"`
	
	// Relasi
	Gateway         Gateway    `gorm:"foreignKey:GatewayID" json:"-"`
	CurrentFirmware *Firmware  `gorm:"foreignKey:CurrentFirmwareID" json:"current_firmware,omitempty"`
}

type OtaLog struct {
	Base
	NodeID           uuid.UUID  `gorm:"type:uuid;not null" json:"node_id"`
	TargetFirmwareID uuid.UUID  `gorm:"type:uuid;not null" json:"target_firmware_id"`
	Status           string     `gorm:"type:varchar(50);not null" json:"status"`
	TriggeredBy      uuid.UUID  `gorm:"type:uuid;not null" json:"triggered_by"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at"`

	Node           Node     `gorm:"foreignKey:NodeID" json:"node,omitempty"`
	TargetFirmware Firmware `gorm:"foreignKey:TargetFirmwareID" json:"target_firmware,omitempty"`
	Admin          User     `gorm:"foreignKey:TriggeredBy" json:"admin,omitempty"`
}

type ShmTelemetry struct {
	Time   time.Time `gorm:"primaryKey;type:timestamp without time zone;not null" json:"time"`
	NodeID uuid.UUID `gorm:"primaryKey;type:uuid;not null" json:"node_id"`
	Ax     float64   `gorm:"not null" json:"ax"`
	Ay     float64   `gorm:"not null" json:"ay"`
	Az     float64   `gorm:"not null" json:"az"`
	Vbatt  float64   `gorm:"not null" json:"vbatt"`
}