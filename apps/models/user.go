package models

type User struct {
	Base
	Email       string `gorm:"uniqueIndex;not null"`
	DisplayName string
	Plan        string `gorm:"not null;default:'free'"`
}

func (User) TableName() string { return "users" }
