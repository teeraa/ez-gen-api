package repository_open_api

import (
	"context"
	"errors"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PointWallet charges AI features against the shared point wallet (director_point_accounts).
// Each AI use is one row in ai_point_charges keyed by (feature, ref); every movement is also
// appended to ai_point_entries.
type PointWallet struct{ db *gorm.DB }

func NewPointWallet(db *gorm.DB) *PointWallet { return &PointWallet{db: db} }

// Charge takes amount points for (feature, ref) and reports whether this call took them.
// A ref that is already charged is not charged again (false, nil), so retrying the same
// request is free; a refunded ref is charged anew. It returns *DirectorInsufficient when
// the balance is too low, and records nothing then.
func (w *PointWallet) Charge(c context.Context, user, feature, ref string, amount int) (bool, error) {
	if amount <= 0 {
		return false, nil
	}
	uid, e := uuid.Parse(user)
	if e != nil {
		return false, ErrNotFound
	}
	charged := false
	e = wrapErr("charge points", w.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		row := models.AIPointCharge{Feature: feature, Ref: ref, UserID: uid, Status: "pending", CreatedAt: now, UpdatedAt: now}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("feature=? AND ref=?", feature, ref).First(&row).Error; e != nil {
			return e
		}
		if row.UserID != uid {
			return ErrConflict
		}
		if row.Status == "charged" {
			return nil
		}
		a := models.DirectorAccount{UserID: uid}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=?", uid).First(&a).Error; e != nil {
			return e
		}
		if a.Balance < int64(amount) {
			return &DirectorInsufficient{Required: amount, Balance: a.Balance}
		}
		if e := tx.Model(&models.DirectorAccount{}).Where("user_id=?", uid).Update("balance", gorm.Expr("balance - ?", amount)).Error; e != nil {
			return e
		}
		if e := tx.Model(&models.AIPointCharge{}).Where("feature=? AND ref=?", feature, ref).Updates(map[string]any{"status": "charged", "amount": amount, "updated_at": now}).Error; e != nil {
			return e
		}
		if e := tx.Create(&models.AIPointEntry{ID: uuid.New(), UserID: uid, Feature: feature, Ref: ref, Kind: "charge", Amount: amount, CreatedAt: now}).Error; e != nil {
			return e
		}
		charged = true
		return nil
	}))
	return charged && e == nil, e
}

// Refund returns the points of a charged (feature, ref). It does nothing when the ref was
// never charged or was already refunded.
func (w *PointWallet) Refund(c context.Context, user, feature, ref string) error {
	uid, e := uuid.Parse(user)
	if e != nil {
		return ErrNotFound
	}
	return wrapErr("refund points", w.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var row models.AIPointCharge
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("feature=? AND ref=? AND user_id=?", feature, ref, uid).First(&row).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if row.Status != "charged" {
			return nil
		}
		now := time.Now()
		if e := tx.Model(&models.AIPointCharge{}).Where("feature=? AND ref=?", feature, ref).Updates(map[string]any{"status": "refunded", "updated_at": now}).Error; e != nil {
			return e
		}
		if row.Amount > 0 {
			if e := tx.Model(&models.DirectorAccount{}).Where("user_id=?", uid).Update("balance", gorm.Expr("balance + ?", row.Amount)).Error; e != nil {
				return e
			}
		}
		return tx.Create(&models.AIPointEntry{ID: uuid.New(), UserID: uid, Feature: feature, Ref: ref, Kind: "refund", Amount: row.Amount, CreatedAt: now}).Error
	}))
}
