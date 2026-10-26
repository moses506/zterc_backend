package models

import "time"

// Base mirrors the Frappe-style document fields the existing ZETRC frontend
// already expects on every generic /api/* record: a string "name" primary
// key, a docstatus workflow (0=Draft, 1=Submitted, 2=Cancelled), and
// creation/modified timestamps.
type Base struct {
	Name      string    `json:"name" gorm:"primaryKey;size:140"`
	Docstatus int       `json:"docstatus" gorm:"default:0"`
	CreatedAt time.Time `json:"creation" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"modified" gorm:"autoUpdateTime"`
}

// Based is implemented by every generic model so the CRUD engine can
// read/write the shared fields without per-model reflection.
type Based interface {
	GetBase() *Base
}

func (b *Base) GetBase() *Base { return b }

const (
	DocstatusDraft     = 0
	DocstatusSubmitted = 1
	DocstatusCancelled = 2
)
